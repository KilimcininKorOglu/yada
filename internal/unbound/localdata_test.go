package unbound

import (
	"context"
	"strings"
	"testing"

	"github.com/kerem/yada/internal/config"
	"github.com/kerem/yada/internal/records"
	"github.com/kerem/yada/internal/transport"
)

func localDataRunner() *fakeRunner {
	return &fakeRunner{
		replies: map[string]transport.Result{
			"local_datas":        {Stdout: "ok"},
			"local_datas_remove": {Stdout: "ok"},
			"local_zones_remove": {Stdout: "ok"},
			"reload_keep_cache":  {Stdout: "ok"},
			"systemctl reload":   {},
			"systemctl restart":  {},
			"is-active":          {Stdout: "active"},
		},
	}
}

func mustRecord(t *testing.T, name string, recType records.Type, value string) records.Record {
	t.Helper()

	rec, err := records.New(name, recType, value, nil)
	if err != nil {
		t.Fatalf("kayıt kurulamadı: %v", err)
	}

	return rec
}

func TestRefreshPrefersLocalDataWhenChangeIsKnown(t *testing.T) {
	r := localDataRunner()

	change := records.Change{
		Added: []records.Record{mustRecord(t, "mail.google.com", records.TypeA, "10.10.10.10")},
	}

	res := Refresh(context.Background(), r, testServer(), config.ReloadAuto, change)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierLocalData {
		t.Errorf("kullanılan kademe = %q, local_data olmalıydı", res.Tier)
	}

	// The config must not be re-read at all when the push works.
	if r.sawCommandContaining("reload_keep_cache") {
		t.Error("local_data başarılıyken config yeniden okutuldu")
	}

	got := r.stdinFor("local_datas")
	if !strings.Contains(got, "mail.google.com. IN A 10.10.10.10") {
		t.Errorf("local_datas girdisi = %q, kayıt beklenen biçimde gönderilmedi", got)
	}
}

func TestRefreshFallsBackWhenControlIsMissing(t *testing.T) {
	r := localDataRunner()
	r.replies["local_datas"] = transport.Result{
		Stdout:   "error: connect failed",
		ExitCode: 1,
	}

	change := records.Change{
		Added: []records.Record{mustRecord(t, "mail.google.com", records.TypeA, "10.10.10.10")},
	}

	res := Refresh(context.Background(), r, testServer(), config.ReloadAuto, change)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierControl {
		t.Errorf("kullanılan kademe = %q, bir sonraki kademeye düşmeliydi", res.Tier)
	}
	if len(res.Attempts) != 2 {
		t.Fatalf("%d kademe denendi, 2 olmalı", len(res.Attempts))
	}
	if res.Attempts[0].Err == nil {
		t.Error("başarısız local_data denemesi hatasız kaydedilmiş")
	}
}

// A bare reload knows nothing about what changed, so pushing individual
// records is not an option and the tier must not even be attempted.
func TestReloadWithoutChangeSkipsLocalData(t *testing.T) {
	r := localDataRunner()

	res := Reload(context.Background(), r, testServer(), config.ReloadAuto)

	if res.Tier != TierControl {
		t.Errorf("kullanılan kademe = %q, local_data kullanılamamalıydı", res.Tier)
	}
	if r.sawCommandContaining("local_datas") {
		t.Error("değişiklik kümesi bilinmezken local_data çalıştırıldı")
	}
}

// Pinning the strategy to local_data cannot be honoured without a change set.
// Failing outright would leave the write unapplied, so it falls back to the
// next tier that reaches the same end state.
func TestPinnedLocalDataFallsBackWithoutChange(t *testing.T) {
	r := localDataRunner()

	res := Reload(context.Background(), r, testServer(), config.ReloadLocalData)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Tier != TierControl {
		t.Errorf("kullanılan kademe = %q, reload_keep_cache olmalıydı", res.Tier)
	}
}

func TestPinnedLocalDataDoesNotFallBackOnFailure(t *testing.T) {
	r := localDataRunner()
	r.replies["local_datas"] = transport.Result{ExitCode: 1}

	change := records.Change{
		Added: []records.Record{mustRecord(t, "mail.google.com", records.TypeA, "10.10.10.10")},
	}

	res := Refresh(context.Background(), r, testServer(), config.ReloadLocalData, change)

	if res.Err == nil {
		t.Fatal("kademe sabitlenmişken başarısızlık hata olarak bildirilmedi")
	}
	if r.sawCommandContaining("reload_keep_cache") {
		t.Error("kademe sabitlenmişken bir alt kademeye düşüldü")
	}
}

// local_data_remove drops every type registered under a name, so the records
// the file still holds for that name have to be pushed back afterwards.
func TestLocalDataRestoresRetainedRecordsAfterRemoval(t *testing.T) {
	r := localDataRunner()

	removed := mustRecord(t, "mail.google.com", records.TypeA, "10.10.10.10")
	retained := mustRecord(t, "mail.google.com", records.TypeTXT, "v=spf1 -all")

	change := records.Change{
		Removed:  []records.Record{removed},
		Retained: []records.Record{retained},
	}

	res := Refresh(context.Background(), r, testServer(), config.ReloadLocalData, change)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}

	if got := r.stdinFor("local_datas_remove"); !strings.Contains(got, "mail.google.com.") {
		t.Errorf("silinecek ad gönderilmedi, girdi = %q", got)
	}

	got := r.stdinFor("unbound-control local_datas 2>&1")
	if !strings.Contains(got, "TXT") {
		t.Errorf("kalan kayıt geri yazılmadı, girdi = %q", got)
	}
	if !strings.Contains(got, `"v=spf1 -all"`) {
		t.Errorf("TXT değeri tırnaklanmadan gönderildi, girdi = %q", got)
	}
}

func TestLocalDataRemovesPrunedZones(t *testing.T) {
	r := localDataRunner()

	change := records.Change{
		Removed:      []records.Record{mustRecord(t, "mail.google.com", records.TypeA, "10.10.10.10")},
		ZonesRemoved: []string{"google.com."},
	}

	res := Refresh(context.Background(), r, testServer(), config.ReloadLocalData, change)

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}

	if got := r.stdinFor("local_zones_remove"); !strings.Contains(got, "google.com.") {
		t.Errorf("kaldırılan zone gönderilmedi, girdi = %q", got)
	}
}

func TestListLocalDataParsesWireFormat(t *testing.T) {
	r := &fakeRunner{
		replies: map[string]transport.Result{
			"list_local_data": {Stdout: "mail.google.com.\t3600\tIN\tA\t10.10.10.10\n" +
				"\nweb.google.com.\tIN\tCNAME\tmail.google.com.\n"},
		},
	}

	recs, err := ListLocalData(context.Background(), r, testServer())
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if len(recs) != 2 {
		t.Fatalf("%d kayıt okundu, 2 olmalı: %+v", len(recs), recs)
	}

	if recs[0].Name != "mail.google.com." || recs[0].Type != records.TypeA || recs[0].Value != "10.10.10.10" {
		t.Errorf("ilk kayıt yanlış ayrıştırıldı: %+v", recs[0])
	}
	if recs[0].TTL == nil || *recs[0].TTL != 3600 {
		t.Errorf("TTL okunmadı: %+v", recs[0])
	}
	if recs[1].Type != records.TypeCNAME {
		t.Errorf("ikinci kayıt yanlış ayrıştırıldı: %+v", recs[1])
	}
}
