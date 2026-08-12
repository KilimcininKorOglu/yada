//go:build !nogui

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

const validConfig = `
servers:
  - name: ns1
    host: 192.0.2.4
    user: user01
`

// newTestApp builds an App with a window but without showing it, which is what
// saveSettings needs to report a failure.
func newTestApp(t *testing.T, configPath string) *App {
	t.Helper()

	fyneApp := test.NewApp()
	t.Cleanup(fyneApp.Quit)

	a := &App{
		fyne:       fyneApp,
		window:     fyneApp.NewWindow("test"),
		configPath: configPath,
		log:        newLogPanel(),
	}

	t.Cleanup(a.log.close)

	return a
}

func TestSaveSettingsWritesAndLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unbound-dns.conf")

	a := newTestApp(t, path)
	status := widget.NewLabel("")

	saved := false
	a.saveSettings(validConfig, status, func() { saved = true })

	if !saved {
		t.Fatalf("kaydetme tamamlanmadı, durum: %q", status.Text)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya yazılmadı: %v", err)
	}
	if string(data) != validConfig {
		t.Errorf("dosya içeriği yazılan metinle aynı değil:\n%s", data)
	}

	if len(a.config().Servers) != 1 {
		t.Errorf("kaydedilen ayar yeniden yüklenmedi, %d sunucu var", len(a.config().Servers))
	}
}

// A file the application can no longer load is worse than a rejected save, so
// invalid content must never reach the disk.
func TestSaveSettingsRefusesInvalidContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unbound-dns.conf")

	if err := os.WriteFile(path, []byte(validConfig), configFileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	a := newTestApp(t, path)
	status := widget.NewLabel("")

	called := false
	a.saveSettings("servers:\n  - host: 192.0.2.4\n    kullanici: user01\n", status, func() { called = true })

	if called {
		t.Error("geçersiz ayar kaydedilmiş sayıldı")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya okunamadı: %v", err)
	}
	if string(data) != validConfig {
		t.Errorf("geçersiz içerik diske yazıldı:\n%s", data)
	}

	// The message has to name the offending key, since that is what the user
	// has to fix.
	if !strings.Contains(status.Text, "kullanici") {
		t.Errorf("durum mesajı hatalı alanı göstermiyor: %q", status.Text)
	}
}

func TestSaveSettingsCreatesMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alt", "dizin", "unbound-dns.conf")

	a := newTestApp(t, path)
	status := widget.NewLabel("")

	a.saveSettings(validConfig, status, nil)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("eksik dizin oluşturulmadı: %v", err)
	}
}

// The file names the servers and the account used to reach them, so it must
// not be world readable.
func TestSaveSettingsKeepsTheFilePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unbound-dns.conf")

	a := newTestApp(t, path)
	a.saveSettings(validConfig, widget.NewLabel(""), nil)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("dosya yazılmadı: %v", err)
	}

	if mode := info.Mode().Perm(); mode != configFileMode {
		t.Errorf("dosya izni = %o, %o olmalı", mode, configFileMode)
	}
}

// An explicitly named file wins over the search order, so a session started
// with --config edits that file and not another one.
func TestConfigTargetPrefersTheExplicitPath(t *testing.T) {
	a := newTestApp(t, "/tmp/explicit.conf")

	got, err := a.configTarget()
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if got != "/tmp/explicit.conf" {
		t.Errorf("hedef = %q", got)
	}
}

func TestLogPanelWritesToFile(t *testing.T) {
	fyneApp := test.NewApp()
	t.Cleanup(fyneApp.Quit)

	path := filepath.Join(t.TempDir(), "gunluk", "unbound-dns.log")

	panel := newLogPanel()
	t.Cleanup(panel.close)

	if err := panel.setFile(path); err != nil {
		t.Fatalf("günlük dosyası açılamadı: %v", err)
	}

	panel.add("ilk satır")
	panel.addf("[%s] ikinci satır", "ns1")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("günlük dosyası okunamadı: %v", err)
	}

	text := string(data)

	for _, want := range []string{"ilk satır", "[ns1] ikinci satır"} {
		if !strings.Contains(text, want) {
			t.Errorf("günlükte %q yok:\n%s", want, text)
		}
	}

	// Each line carries a timestamp, because the file is read long after the
	// session that produced it.
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if !strings.HasPrefix(line, "20") {
			t.Errorf("satır zaman damgasız: %q", line)
		}
	}
}

// The panel is cleared for readability; the file is the record and must
// survive.
func TestLogPanelClearLeavesTheFileIntact(t *testing.T) {
	fyneApp := test.NewApp()
	t.Cleanup(fyneApp.Quit)

	path := filepath.Join(t.TempDir(), "unbound-dns.log")

	panel := newLogPanel()
	t.Cleanup(panel.close)

	if err := panel.setFile(path); err != nil {
		t.Fatalf("günlük dosyası açılamadı: %v", err)
	}

	panel.add("korunmalı")
	panel.clear()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("günlük dosyası okunamadı: %v", err)
	}

	if !strings.Contains(string(data), "korunmalı") {
		t.Errorf("temizleme dosyayı da sildi:\n%s", data)
	}
	if panel.text() != "" {
		t.Errorf("panel temizlenmedi: %q", panel.text())
	}
}

func TestLogPanelSetFileReportsAnUnusablePath(t *testing.T) {
	fyneApp := test.NewApp()
	t.Cleanup(fyneApp.Quit)

	// A directory cannot be opened for appending.
	dir := t.TempDir()

	panel := newLogPanel()
	t.Cleanup(panel.close)

	if err := panel.setFile(dir); err == nil {
		t.Fatal("dizin günlük dosyası olarak kabul edildi")
	}

	if panel.path() != "" {
		t.Errorf("başarısız açılıştan sonra yol tutuldu: %q", panel.path())
	}
}
