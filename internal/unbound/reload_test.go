package unbound

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kerem/yada/internal/config"
	"github.com/kerem/yada/internal/transport"
)

// shortenPolling makes restart cases finish quickly instead of waiting the
// production 30 seconds.
func shortenPolling(t *testing.T) {
	t.Helper()

	oldInterval, oldAttempts := activePollInterval, activePollAttempts
	activePollInterval, activePollAttempts = time.Millisecond, 3

	t.Cleanup(func() {
		activePollInterval, activePollAttempts = oldInterval, oldAttempts
	})
}

func reloadRunner() *fakeRunner {
	return &fakeRunner{
		replies: map[string]transport.Result{
			"reload_keep_cache": {Stdout: "ok"},
			"systemctl reload":  {},
			"systemctl restart": {},
			"is-active":         {Stdout: "active"},
		},
	}
}

func TestReloadUsesLightestTierFirst(t *testing.T) {
	r := reloadRunner()

	res := Reload(context.Background(), r, testServer(), config.ReloadAuto)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierControl {
		t.Errorf("kullanılan kademe = %q, en hafifi seçilmeliydi", res.Tier)
	}
	if len(res.Attempts) != 1 {
		t.Errorf("%d kademe denendi, ilki başarılıysa 1 olmalı", len(res.Attempts))
	}

	// The heavier tiers must not have run at all.
	if r.sawCommandContaining("systemctl reload") || r.sawCommandContaining("systemctl restart") {
		t.Error("hafif kademe başarılıyken ağır kademeler de çalıştı")
	}
}

func TestReloadFallsBackToSignal(t *testing.T) {
	r := reloadRunner()
	r.replies["reload_keep_cache"] = transport.Result{
		Stdout:   "error: connect failed: Connection refused",
		ExitCode: 1,
	}

	res := Reload(context.Background(), r, testServer(), config.ReloadAuto)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierSignal {
		t.Errorf("kullanılan kademe = %q, sinyal reload olmalıydı", res.Tier)
	}
	if r.sawCommandContaining("systemctl restart") {
		t.Error("sinyal reload başarılıyken restart da çalıştı")
	}

	// The reason for falling through has to be recorded, not swallowed.
	if len(res.Attempts) != 2 || res.Attempts[0].Err == nil {
		t.Errorf("başarısız kademe kaydedilmedi: %+v", res.Attempts)
	}
	if !strings.Contains(res.Attempts[0].Output, "Connection refused") {
		t.Errorf("başarısız kademenin çıktısı saklanmadı: %q", res.Attempts[0].Output)
	}
}

func TestReloadFallsBackToRestart(t *testing.T) {
	shortenPolling(t)

	r := reloadRunner()
	r.replies["reload_keep_cache"] = transport.Result{ExitCode: 1}
	r.replies["systemctl reload"] = transport.Result{
		Stdout:   "Failed to reload unbound.service: Job type reload is not applicable",
		ExitCode: 1,
	}

	res := Reload(context.Background(), r, testServer(), config.ReloadAuto)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierRestart {
		t.Errorf("kullanılan kademe = %q", res.Tier)
	}
	if len(res.Attempts) != 3 {
		t.Errorf("%d kademe denendi, 3 beklenirdi", len(res.Attempts))
	}
}

// systemctl reload exits zero even when the daemon dies on the re-read, so the
// service state has to be confirmed or a dead resolver counts as success.
func TestReloadSignalVerifiesServiceStaysActive(t *testing.T) {
	shortenPolling(t)

	r := reloadRunner()
	r.replies["reload_keep_cache"] = transport.Result{ExitCode: 1}
	r.replies["is-active"] = transport.Result{Stdout: "failed", ExitCode: 3}

	res := Reload(context.Background(), r, testServer(), config.ReloadAuto)

	// Signal reload must be judged failed, and restart is then tried and also
	// fails because the service never comes back.
	if len(res.Attempts) < 2 {
		t.Fatalf("kademeler denenmedi: %+v", res.Attempts)
	}
	if res.Attempts[1].Err == nil {
		t.Error("servis ölü olduğu halde sinyal reload başarılı sayıldı")
	}
	if !strings.Contains(res.Attempts[1].Err.Error(), "aktif değil") {
		t.Errorf("hata servisin ölü olduğunu söylemiyor: %v", res.Attempts[1].Err)
	}
	if res.Err == nil {
		t.Error("hiçbir kademe çalışmadığı halde başarı raporlandı")
	}
}

func TestReloadStrategyPinsSingleTier(t *testing.T) {
	r := reloadRunner()
	r.replies["reload_keep_cache"] = transport.Result{ExitCode: 1}

	res := Reload(context.Background(), r, testServer(), config.ReloadControl)

	if res.Err == nil {
		t.Fatal("sabitlenmiş kademe başarısızken hata verilmedi")
	}
	if len(res.Attempts) != 1 {
		t.Errorf("%d kademe denendi, strateji sabitken 1 olmalı", len(res.Attempts))
	}
	if r.sawCommandContaining("systemctl") {
		t.Error("strateji control iken systemctl kullanıldı")
	}
}

func TestReloadStrategyRestartSkipsLighterTiers(t *testing.T) {
	shortenPolling(t)

	r := reloadRunner()

	res := Reload(context.Background(), r, testServer(), config.ReloadRestart)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierRestart {
		t.Errorf("kademe = %q", res.Tier)
	}
	if r.sawCommandContaining("reload_keep_cache") {
		t.Error("strateji restart iken unbound-control denendi")
	}
}

func TestReloadUsesSudo(t *testing.T) {
	r := reloadRunner()

	Reload(context.Background(), r, testServer(), config.ReloadAuto)

	if !r.sawCommandContaining("sudo unbound-control") {
		t.Error("yenileme sudo ile çalıştırılmadı")
	}
}

func TestRestartWaitsForServiceToComeBack(t *testing.T) {
	shortenPolling(t)

	r := reloadRunner()
	r.replies["reload_keep_cache"] = transport.Result{ExitCode: 1}
	r.replies["systemctl reload"] = transport.Result{ExitCode: 1}
	r.replies["is-active"] = transport.Result{Stdout: "activating", ExitCode: 3}

	res := Reload(context.Background(), r, testServer(), config.ReloadAuto)

	if res.Err == nil {
		t.Fatal("servis geri gelmediği halde başarı raporlandı")
	}

	last := res.Attempts[len(res.Attempts)-1]
	if !strings.Contains(last.Err.Error(), "aktif duruma gelmedi") {
		t.Errorf("zaman aşımı hatası beklenirdi: %v", last.Err)
	}
}

func TestTierDescriptionsDistinguishCost(t *testing.T) {
	if !strings.Contains(TierControl.Description(), "cache korundu") {
		t.Errorf("control açıklaması = %q", TierControl.Description())
	}
	if !strings.Contains(TierSignal.Description(), "kesinti yok") {
		t.Errorf("signal açıklaması = %q", TierSignal.Description())
	}
	if !strings.Contains(TierRestart.Description(), "yeniden başlatıldı") {
		t.Errorf("restart açıklaması = %q", TierRestart.Description())
	}
}
