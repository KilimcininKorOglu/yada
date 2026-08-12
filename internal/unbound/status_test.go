package unbound

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/transport"
)

// fakeRunner answers remote commands from a table keyed by a substring of the
// command, so a test states only the commands it cares about.
type fakeRunner struct {
	mu       sync.Mutex
	replies  map[string]transport.Result
	fallback transport.Result
	err      error
	calls    []string
}

func (f *fakeRunner) Run(_ context.Context, _ config.Server, cmd string) (transport.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, cmd)

	if f.err != nil {
		return transport.Result{}, f.err
	}

	for needle, res := range f.replies {
		if strings.Contains(cmd, needle) {
			return res, nil
		}
	}

	return f.fallback, nil
}

func (f *fakeRunner) RunWithStdin(ctx context.Context, srv config.Server, cmd string, _ io.Reader) (transport.Result, error) {
	return f.Run(ctx, srv, cmd)
}

func (f *fakeRunner) sawCommandContaining(needle string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, c := range f.calls {
		if strings.Contains(c, needle) {
			return true
		}
	}

	return false
}

func healthyRunner() *fakeRunner {
	return &fakeRunner{
		replies: map[string]transport.Result{
			"echo unbound-dns-ok": {Stdout: "unbound-dns-ok\n"},
			"unbound -V":          {Stdout: "unbound 1.19.0\n"},
			"is-active":           {Stdout: "active\n"},
			"unbound-checkconf":   {Stdout: "no errors in /etc/unbound/unbound.conf\n"},
			"unbound-control":     {Stdout: "version: 1.19.0\n"},
			"CanReload":           {Stdout: "yes\n"},
		},
	}
}

func testServer() config.Server {
	sudo := true
	return config.Server{
		Name:        "ns1",
		Host:        "10.0.0.1",
		User:        "user01",
		RecordsFile: "/etc/unbound/local_records.conf",
		MainConfig:  "/etc/unbound/unbound.conf",
		Sudo:        &sudo,
	}
}

func TestCheckHealthyServer(t *testing.T) {
	r := healthyRunner()

	st := Check(context.Background(), r, testServer())

	if !st.Reachable {
		t.Fatalf("sunucu ulaşılamaz sayıldı: %v", st.Err)
	}
	if !st.ConfigValid {
		t.Error("config geçersiz sayıldı")
	}
	if !st.ControlAvailable {
		t.Error("unbound-control kullanılamaz sayıldı")
	}
	if !st.ServiceActive {
		t.Error("servis pasif sayıldı")
	}
	if st.Version != "unbound 1.19.0" {
		t.Errorf("sürüm = %q", st.Version)
	}
	if got := st.AvailableTier(); got != "reload_keep_cache" {
		t.Errorf("yenileme kademesi = %q, en hafifi seçilmeliydi", got)
	}
}

// checkconf must target the main config: include splices the records fragment
// into the server clause, so the fragment alone is not a valid config.
func TestCheckValidatesMainConfigNotRecordsFile(t *testing.T) {
	r := healthyRunner()

	Check(context.Background(), r, testServer())

	if !r.sawCommandContaining("unbound-checkconf /etc/unbound/unbound.conf") {
		t.Error("unbound-checkconf ana config ile çağrılmadı")
	}
	if r.sawCommandContaining("unbound-checkconf /etc/unbound/local_records.conf") {
		t.Error("unbound-checkconf kayıt dosyasıyla çağrıldı")
	}
}

func TestCheckUnreachableServer(t *testing.T) {
	r := &fakeRunner{err: errors.New("bağlanılamadı")}

	st := Check(context.Background(), r, testServer())

	if st.Reachable {
		t.Error("ulaşılamayan sunucu ulaşılabilir sayıldı")
	}
	if st.Err == nil {
		t.Error("hata kaydedilmedi")
	}
	if got := st.AvailableTier(); got != "yok" {
		t.Errorf("yenileme kademesi = %q", got)
	}
}

func TestCheckReportsInvalidConfig(t *testing.T) {
	r := healthyRunner()
	r.replies["unbound-checkconf"] = transport.Result{
		Stdout:   "fatal error: syntax error reading /etc/unbound/unbound.conf:42\n",
		ExitCode: 1,
	}

	st := Check(context.Background(), r, testServer())

	if st.ConfigValid {
		t.Error("hatalı config geçerli sayıldı")
	}
	if !strings.Contains(st.ConfigOutput, "syntax error") {
		t.Errorf("checkconf çıktısı saklanmadı: %q", st.ConfigOutput)
	}
}

func TestAvailableTierFallsBackToSignalReload(t *testing.T) {
	r := healthyRunner()
	r.replies["unbound-control"] = transport.Result{
		Stdout:   "error: connect failed: Connection refused\n",
		ExitCode: 1,
	}

	st := Check(context.Background(), r, testServer())

	if st.ControlAvailable {
		t.Error("unbound-control kullanılabilir sayıldı")
	}
	if got := st.AvailableTier(); got != "systemctl reload" {
		t.Errorf("yenileme kademesi = %q, sinyal reload'a düşmeliydi", got)
	}
}

func TestAvailableTierFallsBackToRestart(t *testing.T) {
	r := healthyRunner()
	r.replies["unbound-control"] = transport.Result{ExitCode: 1}
	r.replies["CanReload"] = transport.Result{Stdout: "no\n"}

	st := Check(context.Background(), r, testServer())

	if st.SignalReloadAvailable {
		t.Error("ExecReload tanımsızken sinyal reload kullanılabilir sayıldı")
	}
	if got := st.AvailableTier(); got != "systemctl restart" {
		t.Errorf("yenileme kademesi = %q", got)
	}
}

func TestCheckUsesSudoWhenEnabled(t *testing.T) {
	r := healthyRunner()

	Check(context.Background(), r, testServer())

	if !r.sawCommandContaining("sudo unbound-checkconf") {
		t.Error("sudo açıkken checkconf sudo ile çağrılmadı")
	}
}

func TestCheckSkipsSudoWhenDisabled(t *testing.T) {
	r := healthyRunner()
	srv := testServer()
	off := false
	srv.Sudo = &off

	Check(context.Background(), r, srv)

	if r.sawCommandContaining("sudo ") {
		t.Error("sudo kapalıyken yine de sudo kullanıldı")
	}
}

func TestForEachServerKeepsDeclarationOrder(t *testing.T) {
	cfg := config.Config{
		Behaviour: config.Behaviour{Parallel: true, MaxParallel: 4},
	}

	for _, host := range []string{"a", "b", "c", "d", "e"} {
		cfg.Servers = append(cfg.Servers, config.Server{Host: host})
	}

	got := ForEachServer(context.Background(), cfg, func(_ context.Context, srv config.Server) string {
		return srv.Host
	})

	want := []string{"a", "b", "c", "d", "e"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sonuç sırası bozuldu: %v", got)
		}
	}
}

func TestForEachServerRespectsMaxParallel(t *testing.T) {
	cfg := config.Config{
		Behaviour: config.Behaviour{Parallel: true, MaxParallel: 2},
	}

	for _, host := range []string{"a", "b", "c", "d", "e", "f"} {
		cfg.Servers = append(cfg.Servers, config.Server{Host: host})
	}

	var (
		mu      sync.Mutex
		running int
		peak    int
	)

	ForEachServer(context.Background(), cfg, func(_ context.Context, _ config.Server) bool {
		mu.Lock()
		running++
		peak = max(peak, running)
		mu.Unlock()

		// Hold the slot long enough for the others to pile up if the limit
		// were not enforced.
		for range 200000 {
			_ = 1
		}

		mu.Lock()
		running--
		mu.Unlock()

		return true
	})

	if peak > 2 {
		t.Errorf("aynı anda %d iş çalıştı, en fazla 2 olmalıydı", peak)
	}
}

func TestForEachServerSequentialWhenParallelDisabled(t *testing.T) {
	cfg := config.Config{
		Behaviour: config.Behaviour{Parallel: false},
		Servers: []config.Server{
			{Host: "a"}, {Host: "b"}, {Host: "c"},
		},
	}

	var order []string

	ForEachServer(context.Background(), cfg, func(_ context.Context, srv config.Server) bool {
		order = append(order, srv.Host)
		return true
	})

	if strings.Join(order, ",") != "a,b,c" {
		t.Errorf("sıralı çalışma bozuldu: %v", order)
	}
}
