package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const minimalConfig = `
servers:
  - host: 192.0.2.4
    user: user01
`

func TestDecodeAppliesDefaults(t *testing.T) {
	cfg, err := Decode([]byte(minimalConfig))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	srv := cfg.Servers[0]

	// An unset port must stay zero so ssh can apply ~/.ssh/config itself.
	if srv.Port != 0 {
		t.Errorf("port = %d, belirtilmediğinde 0 kalmalı", srv.Port)
	}
	if srv.RecordsFile != "/etc/unbound/local_records.conf" {
		t.Errorf("records_file = %q", srv.RecordsFile)
	}
	if srv.MainConfig != "/etc/unbound/unbound.conf" {
		t.Errorf("main_config = %q", srv.MainConfig)
	}
	if !srv.UseSudo() {
		t.Error("sudo varsayılan olarak açık olmalı")
	}
	if cfg.Behaviour.ReloadStrategy != ReloadAuto {
		t.Errorf("reload_strategy = %q, beklenen auto", cfg.Behaviour.ReloadStrategy)
	}
	if !cfg.Behaviour.Parallel {
		t.Error("parallel varsayılan olarak açık olmalı")
	}
	if !cfg.Behaviour.BackupBeforeWrite {
		t.Error("backup_before_write varsayılan olarak açık olmalı")
	}
	if cfg.SSH.ConnectTimeout.Std() != 10*time.Second {
		t.Errorf("connect_timeout = %s, beklenen 10s", cfg.SSH.ConnectTimeout.Std())
	}
}

func TestDecodeDefaultsBlockFeedsServers(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
  - host: 192.0.2.5
    user: yonetici
    port: 2222
defaults:
  user: user01
  port: 22
  records_file: /srv/unbound/records.conf
`

	cfg, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if got := cfg.Servers[0].User; got != "user01" {
		t.Errorf("ilk sunucu user = %q, defaults'tan gelmeliydi", got)
	}
	if got := cfg.Servers[0].RecordsFile; got != "/srv/unbound/records.conf" {
		t.Errorf("ilk sunucu records_file = %q", got)
	}

	// A server that sets a field keeps it.
	if got := cfg.Servers[1].Port; got != 2222 {
		t.Errorf("ikinci sunucu port = %d, kendi değeri korunmalıydı", got)
	}
	if got := cfg.Servers[1].User; got != "yonetici" {
		t.Errorf("ikinci sunucu user = %q, kendi değeri korunmalıydı", got)
	}
}

func TestValidateRejectsOutOfRangePort(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: user01
    port: 70000
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("aralık dışı port kabul edildi")
	}
	if !strings.Contains(err.Error(), "aralık dışında") {
		t.Errorf("beklenen port hatası yok: %v", err)
	}
}

func TestValidateRejectsNonASCIIUser(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: özel
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("ASCII dışı kullanıcı adı kabul edildi")
	}
	if !strings.Contains(err.Error(), "geçersiz karakter") {
		t.Errorf("beklenen karakter hatası yok: %v", err)
	}
}

func TestDecodeKeepsExplicitFalse(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: user01
    sudo: false
behaviour:
  parallel: false
  backup_before_write: false
`

	cfg, err := Decode([]byte(input))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if cfg.Servers[0].UseSudo() {
		t.Error("sudo: false yok sayıldı")
	}
	if cfg.Behaviour.Parallel {
		t.Error("parallel: false yok sayıldı")
	}
	if cfg.Behaviour.BackupBeforeWrite {
		t.Error("backup_before_write: false yok sayıldı")
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: user01
    recors_file: /etc/unbound/local_records.conf
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("yazım hatası olan alan sessizce yok sayıldı")
	}
	if !strings.Contains(err.Error(), "recors_file") {
		t.Errorf("hata hangi alanın hatalı olduğunu söylemiyor: %v", err)
	}
}

func TestDecodeRejectsBadDuration(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: user01
ssh:
  connect_timeout: onsaniye
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("geçersiz süre kabul edildi")
	}
	if !strings.Contains(err.Error(), "onsaniye") {
		t.Errorf("hata geçersiz değeri göstermiyor: %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	const input = `
servers:
  - name: ns1
    host: ""
    user: user01
  - name: ns2
    host: 192.0.2.5
    user: user01
    records_file: etc/unbound/records.conf
behaviour:
  reload_strategy: hemen
  max_parallel: 0
log:
  level: sesli
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("geçersiz ayar kabul edildi")
	}

	// Every independent problem must appear, not just the first one.
	for _, want := range []string{
		"host alanı zorunlu",
		"mutlak yol olmalı",
		"reload_strategy",
		"max_parallel",
		"log.level",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("hata %q içermiyor:\n%v", want, err)
		}
	}
}

func TestValidateRejectsShellMetacharactersInPath(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: user01
    records_file: "/etc/unbound/records.conf; rm -rf /"
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("kabuk metakarakteri içeren yol kabul edildi")
	}
	if !strings.Contains(err.Error(), "izin verilmeyen karakter") {
		t.Errorf("beklenen karakter hatası yok: %v", err)
	}
}

func TestValidateRejectsDuplicateHost(t *testing.T) {
	const input = `
servers:
  - host: 192.0.2.4
    user: user01
  - host: 192.0.2.4
    user: user01
`

	_, err := Decode([]byte(input))
	if err == nil {
		t.Fatal("aynı host iki kez tanımlandığı halde kabul edildi")
	}
	if !strings.Contains(err.Error(), "zaten") {
		t.Errorf("beklenen tekrar hatası yok: %v", err)
	}
}

func TestValidateRejectsEmptyServerList(t *testing.T) {
	_, err := Decode([]byte("servers: []\n"))
	if err == nil {
		t.Fatal("boş sunucu listesi kabul edildi")
	}
	if !strings.Contains(err.Error(), "en az bir sunucu") {
		t.Errorf("beklenen hata yok: %v", err)
	}
}

func TestFindInPrefersFirstDirectory(t *testing.T) {
	exeDir := t.TempDir()
	homeDir := t.TempDir()

	writeConfig(t, exeDir)
	writeConfig(t, homeDir)

	got, err := FindIn([]string{exeDir, homeDir})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if want := filepath.Join(exeDir, FileName); got != want {
		t.Errorf("bulunan = %q, beklenen %q (uygulama yanı öncelikli olmalı)", got, want)
	}
}

func TestFindInFallsBackToSecondDirectory(t *testing.T) {
	exeDir := t.TempDir()
	homeDir := t.TempDir()

	writeConfig(t, homeDir)

	got, err := FindIn([]string{exeDir, homeDir})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if want := filepath.Join(homeDir, FileName); got != want {
		t.Errorf("bulunan = %q, beklenen %q", got, want)
	}
}

func TestFindInReportsNotFound(t *testing.T) {
	_, err := FindIn([]string{t.TempDir()})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("hata = %v, beklenen ErrNotFound", err)
	}
}

func TestFindInIgnoresDirectoryWithConfigName(t *testing.T) {
	dir := t.TempDir()

	if err := os.Mkdir(filepath.Join(dir, FileName), 0o755); err != nil {
		t.Fatalf("dizin oluşturulamadı: %v", err)
	}

	if _, err := FindIn([]string{dir}); !errors.Is(err, ErrNotFound) {
		t.Errorf("aynı isimli dizin dosya sanıldı: %v", err)
	}
}

func TestLoadRecordsSourcePath(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir)
	path := filepath.Join(dir, FileName)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if cfg.SourcePath != path {
		t.Errorf("SourcePath = %q, beklenen %q", cfg.SourcePath, path)
	}
}

// The embedded example is what config init writes, so a mistake in it would
// hand the user a file that fails to load on their first run.
func TestEmbeddedExampleIsValid(t *testing.T) {
	cfg, err := Decode(Example)
	if err != nil {
		t.Fatalf("gömülü örnek ayar geçersiz: %v", err)
	}

	if len(cfg.Servers) == 0 {
		t.Error("örnek ayarda sunucu tanımı yok")
	}

	// The example documents every key, so it must also survive KnownFields.
	for _, srv := range cfg.Servers {
		if srv.RecordsFile == "" || srv.MainConfig == "" {
			t.Errorf("%s: örnek ayarda dosya yolları eksik", srv.Label())
		}
	}
}

func writeConfig(t *testing.T, dir string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(minimalConfig), 0o600); err != nil {
		t.Fatalf("ayar dosyası yazılamadı: %v", err)
	}
}
