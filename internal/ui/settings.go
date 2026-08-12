//go:build !nogui

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kerem/unbound-dns/internal/config"
)

func (a *App) buildSettingsTab() fyne.CanvasObject {
	summary := widget.NewMultiLineEntry()
	summary.Wrapping = fyne.TextWrapWord
	summary.Disable()

	refresh := func() {
		summary.SetText(a.settingsSummary())
	}

	refresh()

	reloadButton := widget.NewButton("Ayarı yeniden yükle", func() {
		a.loadConfig()
		refresh()

		// Rebuilding the tabs makes the other screens pick up the new server
		// list; they read it once when they are constructed.
		a.window.SetContent(a.buildTabs())
	})

	createButton := widget.NewButton("Örnek ayar dosyası oluştur", func() {
		a.createExampleConfig(refresh)
	})

	openDirButton := widget.NewButton("Ayar dosyasının yolunu kopyala", func() {
		path := a.config().SourcePath
		if path == "" {
			dialog.ShowInformation("Dosya yok", "Henüz yüklenmiş bir ayar dosyası yok.", a.window)
			return
		}

		fyne.CurrentApp().Clipboard().SetContent(path)
		a.log.addf("Ayar dosyasının yolu panoya kopyalandı: %s", path)
	})

	toolbar := container.NewHBox(reloadButton, createButton, openDirButton)

	help := widget.NewRichTextFromMarkdown(
		"Ayar dosyası iki konumda aranır, ilk bulunan kullanılır:\n\n" +
			"1. Uygulamanın bulunduğu dizin\n" +
			"2. Kullanıcı ana dizini\n\n" +
			"Dosyayı düzenledikten sonra **Ayarı yeniden yükle** düğmesine basın.")

	return container.NewBorder(
		container.NewVBox(toolbar, widget.NewSeparator()),
		help, nil, nil,
		summary,
	)
}

func (a *App) settingsSummary() string {
	cfg := a.config()

	if len(cfg.Servers) == 0 {
		var b strings.Builder

		b.WriteString("Yüklü ayar yok.\n\nAranan konumlar:\n")

		for _, dir := range config.SearchDirs() {
			fmt.Fprintf(&b, "  %s\n", filepath.Join(dir, config.FileName))
		}

		return b.String()
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Ayar dosyası: %s\n\n", cfg.SourcePath)
	fmt.Fprintf(&b, "Yenileme stratejisi : %s\n", cfg.Behaviour.ReloadStrategy)
	fmt.Fprintf(&b, "Eşzamanlılık        : %t (en fazla %d)\n", cfg.Behaviour.Parallel, cfg.Behaviour.MaxParallel)
	fmt.Fprintf(&b, "Yazmadan önce yedek : %t\n", cfg.Behaviour.BackupBeforeWrite)
	fmt.Fprintf(&b, "Yıkıcı işlemde onay : %t\n", cfg.Behaviour.ConfirmDestructive)
	fmt.Fprintf(&b, "ssh                 : %s (zaman aşımı %s)\n\n", cfg.SSH.Binary, cfg.SSH.ConnectTimeout.Std())

	fmt.Fprintf(&b, "Sunucular (%d):\n", len(cfg.Servers))

	for _, srv := range cfg.Servers {
		port := "ssh varsayılanı"
		if srv.Port > 0 {
			port = fmt.Sprintf("%d", srv.Port)
		}

		fmt.Fprintf(&b, "\n  %s\n", srv.Label())
		fmt.Fprintf(&b, "    adres      : %s@%s (port: %s)\n", srv.User, srv.Host, port)
		fmt.Fprintf(&b, "    kayıtlar   : %s\n", srv.RecordsFile)
		fmt.Fprintf(&b, "    ana config : %s\n", srv.MainConfig)
		fmt.Fprintf(&b, "    sudo       : %t\n", srv.UseSudo())
	}

	return b.String()
}

// createExampleConfig writes the starter file next to the executable, which is
// the first location the loader checks.
func (a *App) createExampleConfig(onDone func()) {
	dir, err := config.ExecutableDir()
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	path := filepath.Join(dir, config.FileName)

	write := func() {
		if err := os.WriteFile(path, config.Example, 0o600); err != nil {
			dialog.ShowError(fmt.Errorf("ayar dosyası yazılamadı: %w", err), a.window)
			return
		}

		a.log.addf("Örnek ayar dosyası oluşturuldu: %s", path)

		dialog.ShowInformation("Oluşturuldu",
			fmt.Sprintf("%s\n\nSunucu adreslerini düzenleyip «Ayarı yeniden yükle» deyin.", path),
			a.window)

		if onDone != nil {
			onDone()
		}
	}

	if _, err := os.Stat(path); err == nil {
		dialog.ShowConfirm("Dosya zaten var",
			fmt.Sprintf("%s üzerine yazılsın mı?", path),
			func(ok bool) {
				if ok {
					write()
				}
			}, a.window)

		return
	}

	write()
}
