//go:build !nogui

// Package ui builds the Fyne desktop interface. It holds no business logic of
// its own: every operation goes through the same internal packages the CLI
// uses, so the two cannot drift apart.
package ui

import (
	"context"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/transport"
)

// App holds everything the screens share.
type App struct {
	fyne   fyne.App
	window fyne.Window

	// configPath is set when the user named a file explicitly, in which case
	// the search order is bypassed.
	configPath string

	mu     sync.RWMutex
	cfg    config.Config
	runner transport.Runner

	log *logPanel

	// cancel aborts the operation currently running, if any.
	cancelMu sync.Mutex
	cancel   context.CancelFunc
}

// Run builds the window and blocks until it closes. An empty configPath leaves
// the usual search order in place.
func Run(version, configPath string) {
	a := &App{
		fyne:       app.NewWithID("dev.kerem.unbound-dns"),
		configPath: configPath,
	}

	a.window = a.fyne.NewWindow("Unbound DNS Yöneticisi " + version)
	a.window.Resize(fyne.NewSize(1100, 700))

	a.log = newLogPanel()

	a.loadConfig()
	a.window.SetContent(a.buildTabs())
	a.window.ShowAndRun()
}

// loadConfig reads the configuration. A failure is not fatal: the settings tab
// has to stay reachable so the user can fix the file from inside the app.
func (a *App) loadConfig() {
	cfg, err := a.readConfig()
	if err != nil {
		a.log.addf("Ayar okunamadı: %v", err)
		return
	}

	a.mu.Lock()
	a.cfg = cfg
	a.runner = transport.NewSSHRunner(cfg.SSH)
	a.mu.Unlock()

	a.log.addf("Ayar yüklendi: %s (%d sunucu)", cfg.SourcePath, len(cfg.Servers))
}

// readConfig honours an explicitly named file and otherwise falls back to the
// search order.
func (a *App) readConfig() (config.Config, error) {
	if a.configPath != "" {
		return config.Load(a.configPath)
	}

	return config.LoadDefault()
}

// config returns a snapshot of the current configuration.
func (a *App) config() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.cfg
}

func (a *App) transportRunner() transport.Runner {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.runner
}

// configured reports whether a usable configuration is loaded, and tells the
// user where to fix it when not.
func (a *App) configured() bool {
	if len(a.config().Servers) > 0 {
		return true
	}

	dialog.ShowInformation("Ayar yok",
		"Kullanılabilir bir ayar dosyası yüklenmedi.\nAyarlar sekmesinden dosyayı oluşturun.",
		a.window)

	return false
}

func (a *App) buildTabs() fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItem("Sunucular", a.buildServersTab()),
		container.NewTabItem("Kayıtlar", a.buildRecordsTab()),
		container.NewTabItem("Fark", a.buildDiffTab()),
		container.NewTabItem("Toplu İşlem", a.buildBulkTab()),
		container.NewTabItem("Ayarlar", a.buildSettingsTab()),
		container.NewTabItem("Günlük", a.log.canvas()),
	)

	tabs.SetTabLocation(container.TabLocationLeading)

	return tabs
}

// run executes work off the UI goroutine so the window keeps responding, and
// wires the cancel button to the context.
//
// done runs back on the UI goroutine, which is where Fyne widgets must be
// touched from.
func (a *App) run(title string, work func(context.Context) error, done func()) {
	ctx, cancel := context.WithCancel(context.Background())

	a.cancelMu.Lock()
	a.cancel = cancel
	a.cancelMu.Unlock()

	progress := dialog.NewCustomWithoutButtons(title, widget.NewProgressBarInfinite(), a.window)

	cancelButton := widget.NewButton("İptal", func() {
		cancel()
	})
	progress.SetButtons([]fyne.CanvasObject{cancelButton})
	progress.Show()

	go func() {
		err := work(ctx)

		fyne.Do(func() {
			progress.Hide()
			cancel()

			if err != nil {
				if ctx.Err() != nil {
					a.log.add("İşlem iptal edildi.")
				} else {
					a.log.addf("HATA: %v", err)
					dialog.ShowError(err, a.window)
				}
			}

			if done != nil {
				done()
			}
		})
	}()
}

// confirmDestructive asks before an irreversible change, showing how many
// records are affected so the answer is informed.
func (a *App) confirmDestructive(title, detail string, count int, onConfirm func()) {
	if !a.config().Behaviour.ConfirmDestructive {
		onConfirm()
		return
	}

	message := fmt.Sprintf("%s\n\nEtkilenecek kayıt sayısı: %d", detail, count)

	dialog.ShowConfirm(title, message, func(ok bool) {
		if ok {
			onConfirm()
		}
	}, a.window)
}
