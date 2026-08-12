//go:build !nogui

package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kerem/unbound-dns/internal/config"
)

// configFileMode keeps the file readable only by its owner, because it names
// the servers and the account used to reach them.
const configFileMode fs.FileMode = 0o600

func (a *App) buildSettingsTab() fyne.CanvasObject {
	editor := widget.NewMultiLineEntry()
	editor.Wrapping = fyne.TextWrapOff
	editor.SetPlaceHolder("Ayar dosyası burada düzenlenir.")

	pathLabel := widget.NewLabel("")
	pathLabel.TextStyle = fyne.TextStyle{Bold: true}

	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	// A disabled Entry would be the obvious choice for read-only text, but Fyne
	// renders it in the disabled colour, which is too faint to read. A Label
	// keeps the normal foreground; the copy button below makes up for the text
	// no longer being selectable.
	summary := widget.NewLabel("")
	summary.TextStyle = fyne.TextStyle{Monospace: true}
	summary.Wrapping = fyne.TextWrapOff

	// loadEditor fills the editor from disk, discarding unsaved edits. It is
	// also what runs on first build, so the tab always opens on the real file
	// rather than on whatever was last typed.
	loadEditor := func() {
		path, err := a.configTarget()
		if err != nil {
			status.SetText(err.Error())
			return
		}

		pathLabel.SetText(path)

		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			editor.SetText(string(config.Example))
			status.SetText("Dosya yok. Örnek içerik yüklendi, düzenleyip kaydedin.")

		case err != nil:
			status.SetText(fmt.Sprintf("Dosya okunamadı: %v", err))

		default:
			editor.SetText(string(data))
			status.SetText("Dosya okundu.")
		}
	}

	refreshSummary := func() {
		summary.SetText(a.settingsSummary())
	}

	loadEditor()
	refreshSummary()

	validateButton := widget.NewButton("Doğrula", func() {
		if _, err := config.Decode([]byte(editor.Text)); err != nil {
			status.SetText("Geçersiz:\n" + err.Error())
			return
		}

		status.SetText("Ayar geçerli. Kaydedebilirsiniz.")
	})

	saveButton := widget.NewButton("Kaydet ve yükle", func() {
		a.saveSettings(editor.Text, status, func() {
			refreshSummary()

			// Rebuilding the tabs makes the other screens pick up the new
			// server list; they read it once when they are constructed.
			a.window.SetContent(a.buildTabs())
		})
	})

	revertButton := widget.NewButton("Dosyadan geri al", func() {
		loadEditor()
	})

	reloadButton := widget.NewButton("Ayarı yeniden yükle", func() {
		a.loadConfig()
		loadEditor()
		refreshSummary()
		a.window.SetContent(a.buildTabs())
	})

	copyPathButton := widget.NewButton("Yolu kopyala", func() {
		path, err := a.configTarget()
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}

		fyne.CurrentApp().Clipboard().SetContent(path)
		a.log.addf("Ayar dosyasının yolu panoya kopyalandı: %s", path)
	})

	exampleButton := widget.NewButton("Örneği yükle", func() {
		dialog.ShowConfirm("Örneği yükle",
			"Düzenleyicideki içerik örnek ayarla değiştirilecek. Dosya, siz kaydedene kadar değişmez.",
			func(ok bool) {
				if !ok {
					return
				}

				editor.SetText(string(config.Example))
				status.SetText("Örnek içerik yüklendi. Kaydetmeden dosya değişmez.")
			}, a.window)
	})

	toolbar := container.NewHBox(
		saveButton, validateButton, revertButton, reloadButton, exampleButton, copyPathButton)

	header := container.NewVBox(
		toolbar,
		container.NewHBox(widget.NewLabel("Dosya:"), pathLabel),
		widget.NewSeparator(),
	)

	summaryHeader := container.NewVBox(
		container.NewHBox(
			widget.NewLabel("Yüklü ayarın özeti"),
			widget.NewButton("Özeti kopyala", func() {
				fyne.CurrentApp().Clipboard().SetContent(summary.Text)
			}),
		),
		widget.NewSeparator(),
	)

	// The editor gets the larger half; the summary is there to confirm what
	// the file actually resolved to after defaults were applied.
	split := container.NewVSplit(
		container.NewBorder(header, status, nil, nil, editor),
		container.NewBorder(summaryHeader, nil, nil, nil, container.NewScroll(summary)),
	)
	split.SetOffset(0.62)

	return split
}

// saveSettings validates before writing, so a typo cannot leave the
// application with a file it can no longer load.
func (a *App) saveSettings(content string, status *widget.Label, onDone func()) {
	if _, err := config.Decode([]byte(content)); err != nil {
		status.SetText("Kaydedilmedi, ayar geçersiz:\n" + err.Error())
		dialog.ShowError(fmt.Errorf("ayar geçersiz, dosya değiştirilmedi:\n\n%w", err), a.window)

		return
	}

	path, err := a.configTarget()
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			dialog.ShowError(fmt.Errorf("dizin oluşturulamadı (%s): %w", dir, err), a.window)
			return
		}
	}

	if err := os.WriteFile(path, []byte(content), configFileMode); err != nil {
		dialog.ShowError(fmt.Errorf("ayar dosyası yazılamadı (%s): %w", path, err), a.window)
		return
	}

	a.log.addf("Ayar dosyası kaydedildi: %s", path)

	// The file is known to parse, so the reload cannot leave the application
	// worse off than it was.
	a.loadConfig()

	status.SetText("Kaydedildi ve yüklendi: " + path)

	if onDone != nil {
		onDone()
	}
}

// configTarget is the file the editor reads and writes.
//
// An explicitly named file wins, then the one the current configuration came
// from, and finally the directory beside the executable, which is the first
// place the loader looks.
func (a *App) configTarget() (string, error) {
	if a.configPath != "" {
		return a.configPath, nil
	}

	if path := a.config().SourcePath; path != "" {
		return path, nil
	}

	dir, err := config.ExecutableDir()
	if err != nil {
		return "", fmt.Errorf("ayar dosyasının yeri belirlenemedi: %w", err)
	}

	return filepath.Join(dir, config.FileName), nil
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
	fmt.Fprintf(&b, "ssh                 : %s (zaman aşımı %s)\n", cfg.SSH.Binary, cfg.SSH.ConnectTimeout.Std())

	logFile := cfg.Log.File
	if logFile == "" {
		logFile = "yok (günlük yalnızca pencerede)"
	}
	fmt.Fprintf(&b, "günlük dosyası      : %s\n\n", logFile)

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
