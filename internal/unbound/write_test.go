package unbound

import (
	"context"
	"strings"
	"testing"

	"github.com/kerem/yada/internal/records"
	"github.com/kerem/yada/internal/transport"
)

const baseRecords = `# Yerel kayıtlar
         local-zone: "google.com." transparent
         local-data: "mail.google.com. IN A 10.10.10.10"
`

// writeRunner answers the commands a write performs. checkconf succeeds unless
// a test overrides it.
func writeRunner() *fakeRunner {
	return &fakeRunner{
		replies: map[string]transport.Result{
			"cat ":              {Stdout: baseRecords},
			"cp -p":             {},
			"tee ":              {},
			"unbound-checkconf": {Stdout: "no errors"},
		},
	}
}

// prepared parses the base file and applies a change, returning the original
// bytes and the modified file, which is what Write compares.
func prepared(t *testing.T, change func(*records.File)) ([]byte, *records.File) {
	t.Helper()

	file, err := records.Parse([]byte(baseRecords))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	before := file.Bytes()
	change(file)

	return before, file
}

func addRecord(t *testing.T) func(*records.File) {
	t.Helper()

	return func(f *records.File) {
		rec, err := records.New("yeni.google.com", records.TypeA, "10.20.20.20", nil)
		if err != nil {
			t.Fatalf("kayıt oluşturulamadı: %v", err)
		}
		if err := f.Add(rec); err != nil {
			t.Fatalf("ekleme hatası: %v", err)
		}
	}
}

func TestWriteSucceeds(t *testing.T) {
	r := writeRunner()
	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if !res.Changed {
		t.Error("değişiklik yapılmadı olarak raporlandı")
	}
	if res.RolledBack {
		t.Error("başarılı yazmadan sonra geri alma yapıldı")
	}

	if !r.sawCommandContaining("cp -p /etc/unbound/local_records.conf /etc/unbound/local_records.conf.bak") {
		t.Error("yazmadan önce yedek alınmadı")
	}
	if !r.sawCommandContaining("tee /etc/unbound/local_records.conf") {
		t.Error("dosya tee ile yazılmadı")
	}
	if !strings.Contains(r.receivedStdin(), "yeni.google.com") {
		t.Errorf("yeni içerik stdin ile gönderilmedi: %q", r.receivedStdin())
	}
}

// The write has to report which records moved, not only which lines, because
// that is what lets the refresh push the change into the running daemon
// instead of making it re-read the whole config.
func TestWriteReportsRecordLevelChange(t *testing.T) {
	r := writeRunner()
	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}

	if len(res.Change.Added) != 1 {
		t.Fatalf("kayıt seviyesinde %d ekleme bildirildi, 1 olmalı: %+v", len(res.Change.Added), res.Change.Added)
	}
	if res.Change.Added[0].Name != "yeni.google.com." {
		t.Errorf("eklenen kayıt = %q, yeni.google.com. olmalı", res.Change.Added[0].Name)
	}
	if len(res.Change.Removed) != 0 {
		t.Errorf("silinen bildirildi: %+v", res.Change.Removed)
	}
}

// The records must travel over stdin, never inside the command string, so the
// remote shell never sees the quotes they contain.
func TestWriteSendsContentOverStdinNotArguments(t *testing.T) {
	r := writeRunner()
	before, file := prepared(t, addRecord(t))

	Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	for _, call := range r.calls {
		if strings.Contains(call, "local-data:") {
			t.Errorf("kayıt içeriği komut satırına gömüldü: %q", call)
		}
	}
}

func TestWriteRollsBackWhenValidationFails(t *testing.T) {
	r := writeRunner()
	r.replies["unbound-checkconf"] = transport.Result{
		Stdout:   "fatal error: syntax error reading /etc/unbound/unbound.conf:12",
		ExitCode: 1,
	}

	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	if res.Err == nil {
		t.Fatal("doğrulama başarısızken hata verilmedi")
	}
	if !res.RolledBack {
		t.Error("geri alma yapılmadı")
	}
	if res.Changed {
		t.Error("geri alınan yazma değişiklik olarak raporlandı")
	}
	if !strings.Contains(res.CheckOutput, "syntax error") {
		t.Errorf("checkconf çıktısı saklanmadı: %q", res.CheckOutput)
	}

	// The restore must copy the backup back over the records file.
	if !r.sawCommandContaining("cp -p /etc/unbound/local_records.conf.bak /etc/unbound/local_records.conf") {
		t.Error("yedek geri yüklenmedi")
	}
}

// Losing both the write and the rollback is the worst case, so the message has
// to name the backup file the operator needs.
func TestWriteReportsFailedRollback(t *testing.T) {
	r := writeRunner()
	r.replies["unbound-checkconf"] = transport.Result{Stdout: "fatal error", ExitCode: 1}
	r.replies["cp -p /etc/unbound/local_records.conf.bak"] = transport.Result{
		Stderr:   "cp: cannot create regular file: Read-only file system",
		ExitCode: 1,
	}

	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	if res.RolledBack {
		t.Error("başarısız geri alma başarılı sayıldı")
	}
	if res.Err == nil {
		t.Fatal("hata bildirilmedi")
	}

	msg := res.Err.Error()
	if !strings.Contains(msg, "bozuk durumda") {
		t.Errorf("hata sunucunun bozuk kaldığını söylemiyor: %v", res.Err)
	}
	if !strings.Contains(msg, "local_records.conf.bak") {
		t.Errorf("hata yedek dosyanın yolunu vermiyor: %v", res.Err)
	}
}

func TestWriteRefusesWhenBackupFails(t *testing.T) {
	r := writeRunner()
	r.replies["cp -p"] = transport.Result{
		Stderr:   "cp: cannot stat: No such file or directory",
		ExitCode: 1,
	}

	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	if res.Err == nil {
		t.Fatal("yedek alınamadığı halde devam edildi")
	}
	if res.Changed {
		t.Error("yedeksiz yazma yapıldı")
	}
	if r.sawCommandContaining("tee ") {
		t.Error("yedek başarısızken dosya yine de yazıldı")
	}
}

// Without a backup there is nothing to restore, so the failure message must
// tell the operator the server needs manual repair.
func TestWriteWarnsWhenValidationFailsWithoutBackup(t *testing.T) {
	r := writeRunner()
	r.replies["unbound-checkconf"] = transport.Result{Stdout: "fatal error", ExitCode: 1}

	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: false})

	if res.RolledBack {
		t.Error("yedek yokken geri alma raporlandı")
	}
	if !strings.Contains(res.Err.Error(), "elle düzeltin") {
		t.Errorf("hata elle müdahale gerektiğini söylemiyor: %v", res.Err)
	}
}

func TestWriteDryRunTouchesNothing(t *testing.T) {
	r := writeRunner()
	before, file := prepared(t, addRecord(t))

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true, DryRun: true})

	if res.Err != nil {
		t.Fatalf("beklenmeyen hata: %v", res.Err)
	}
	if res.Changed {
		t.Error("dry-run değişiklik yaptı olarak raporlandı")
	}

	for _, forbidden := range []string{"tee ", "cp -p"} {
		if r.sawCommandContaining(forbidden) {
			t.Errorf("dry-run %q komutunu çalıştırdı", forbidden)
		}
	}

	if res.Diff.Empty() {
		t.Error("dry-run farkı göstermedi")
	}
	if len(res.Diff.Added) != 1 {
		t.Errorf("fark %d eklenen satır gösterdi, beklenen 1: %v", len(res.Diff.Added), res.Diff.Added)
	}
}

func TestWriteSkipsUnchangedFile(t *testing.T) {
	r := writeRunner()
	before, file := prepared(t, func(*records.File) {})

	res := Write(context.Background(), r, testServer(), before, file, WriteOptions{Backup: true})

	if res.Changed {
		t.Error("değişiklik olmadığı halde yazma yapıldı")
	}
	if r.sawCommandContaining("tee ") {
		t.Error("değişiklik yokken dosya yazıldı")
	}
	if !res.Diff.Empty() {
		t.Errorf("boş fark bekleniyordu: %v", res.Diff)
	}
}

func TestDiffContent(t *testing.T) {
	before := []byte("bir\niki\nüç\n")
	after := []byte("bir\nüç\ndört\n")

	d := DiffContent(before, after)

	if len(d.Added) != 1 || d.Added[0] != "dört" {
		t.Errorf("eklenenler = %v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "iki" {
		t.Errorf("silinenler = %v", d.Removed)
	}

	rendered := d.String()
	if !strings.Contains(rendered, "- iki") || !strings.Contains(rendered, "+ dört") {
		t.Errorf("fark gösterimi = %q", rendered)
	}
}

// Duplicate lines must be counted, not collapsed, or removing one of two
// identical records would look like no change at all.
func TestDiffContentCountsDuplicateLines(t *testing.T) {
	before := []byte("aynı\naynı\n")
	after := []byte("aynı\n")

	d := DiffContent(before, after)

	if len(d.Removed) != 1 {
		t.Errorf("silinenler = %v, tekrarlı satır sayılmadı", d.Removed)
	}
	if len(d.Added) != 0 {
		t.Errorf("eklenenler = %v", d.Added)
	}
}

func TestApplyReportsPerServerFailure(t *testing.T) {
	r := writeRunner()
	r.replies["cat "] = transport.Result{Stderr: "cat: No such file or directory", ExitCode: 1}

	cfg := twoServerConfig()

	results := Apply(context.Background(), r, cfg, WriteOptions{Backup: true}, func(f *records.File) error {
		rec, _ := records.New("h.example.com", records.TypeA, "10.0.0.1", nil)
		return f.Add(rec)
	})

	if len(results) != 2 {
		t.Fatalf("%d sonuç döndü", len(results))
	}

	for _, res := range results {
		if res.Err == nil {
			t.Errorf("[%s] okunamayan dosya hata vermedi", res.Server.Label())
		}
	}
}
