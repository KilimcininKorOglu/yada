//go:build !nogui

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/KilimcininKorOglu/yada/internal/sshsetup"
	"github.com/KilimcininKorOglu/yada/internal/unbound"
)

// serverForm is what the user typed. The key is kept apart from everything
// that gets displayed or logged.
type serverForm struct {
	name string
	host string
	user string
	port int
	key  []byte
}

// server builds the entry written to the configuration file.
func (f serverForm) server() config.Server {
	return config.Server{
		Name: f.name,
		Host: f.host,
		User: f.user,
		Port: f.port,
	}
}

// label is what the user calls this server.
func (f serverForm) label() string {
	if f.name != "" {
		return f.name
	}

	return f.host
}

// showAddServerDialog collects a server and its key, then writes both the ssh
// files and the configuration.
func (a *App) showAddServerDialog(onDone func()) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("ns1")

	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("192.0.2.4")

	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("user01")

	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("boş bırakılırsa ssh kendi çözer")

	keyEntry := widget.NewMultiLineEntry()
	keyEntry.SetPlaceHolder("-----BEGIN OPENSSH PRIVATE KEY-----")
	keyEntry.Wrapping = fyne.TextWrapOff

	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	read := func() (serverForm, error) {
		return readServerForm(nameEntry.Text, hostEntry.Text, userEntry.Text, portEntry.Text, keyEntry.Text)
	}

	showValidation := func(string) {
		if _, err := read(); err != nil {
			errorLabel.SetText(err.Error())
			return
		}

		errorLabel.SetText("")
	}

	hostEntry.OnChanged = showValidation
	userEntry.OnChanged = showValidation
	portEntry.OnChanged = showValidation

	form := widget.NewForm(
		widget.NewFormItem("Ad", nameEntry),
		widget.NewFormItem("Adres", hostEntry),
		widget.NewFormItem("Kullanıcı", userEntry),
		widget.NewFormItem("Port", portEntry),
	)

	hint := widget.NewLabel("Private key dosyanızın içeriğini yapıştırın. " +
		"Eşleşen public key sunucudaki authorized_keys dosyasında zaten olmalı.")
	hint.Wrapping = fyne.TextWrapWord

	content := container.NewBorder(
		container.NewVBox(form, hint),
		errorLabel, nil, nil,
		container.NewScroll(keyEntry),
	)

	d := dialog.NewCustomConfirm("Sunucu ekle", "Devam", "Vazgeç", content, func(ok bool) {
		if !ok {
			return
		}

		entered, err := read()
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}

		a.startServerSetup(entered, onDone)
	}, a.window)

	d.Resize(fyne.NewSize(620, 560))
	d.Show()
}

// readServerForm turns the form fields into a validated request.
func readServerForm(name, host, user, port, key string) (serverForm, error) {
	entered := serverForm{
		name: strings.TrimSpace(name),
		host: strings.TrimSpace(host),
		user: strings.TrimSpace(user),
		key:  []byte(key),
	}

	if entered.host == "" {
		return serverForm{}, errors.New("adres zorunlu")
	}
	if entered.user == "" {
		return serverForm{}, errors.New("kullanıcı zorunlu")
	}

	if trimmed := strings.TrimSpace(port); trimmed != "" {
		parsed, err := strconv.Atoi(trimmed)
		if err != nil || parsed < 1 || parsed > 65535 {
			return serverForm{}, fmt.Errorf("port 1-65535 aralığında bir sayı olmalı, %q verilmiş", trimmed)
		}

		entered.port = parsed
	}

	if len(strings.TrimSpace(string(entered.key))) == 0 {
		return serverForm{}, errors.New("private key zorunlu")
	}

	// The configuration package is the authority on what a server may hold, so
	// the same rules run here rather than a second copy of them.
	if err := entered.server().ValidateFields(); err != nil {
		return serverForm{}, err
	}

	return entered, nil
}

// startServerSetup checks the server, shows its host key, and writes
// everything once the user has accepted it.
//
// The scan and the writing are two separate runs, because the progress dialog
// stays up for as long as its work does and the fingerprint has to be shown in
// between.
func (a *App) startServerSetup(entered serverForm, onDone func()) {
	var (
		scan    sshsetup.Scan
		state   sshsetup.KnownHostsState
		scanned bool
	)

	a.run("Sunucu denetleniyor", func(ctx context.Context) error {
		paths, err := sshsetup.Locate(a.config().SSH.ConfigFile)
		if err != nil {
			return err
		}

		scan, err = sshsetup.ScanHost(ctx, entered.host, entered.port)
		if err != nil {
			return err
		}

		state, err = sshsetup.CheckKnownHosts(paths.KnownHosts, scan)
		if err != nil {
			return err
		}

		scanned = true

		return nil
	}, func() {
		if !scanned {
			return
		}

		if state == sshsetup.HostChanged {
			a.warnHostKeyChanged(entered, scan)
			return
		}

		a.confirmHostKey(entered, scan, state, onDone)
	})
}

// warnHostKeyChanged reports a host whose key no longer matches the one on
// record. Nothing is written: the two explanations are a rebuilt server and a
// connection reaching somewhere else, and only the user can tell them apart.
func (a *App) warnHostKeyChanged(entered serverForm, scan sshsetup.Scan) {
	a.log.addf("[%s] host key known_hosts kaydıyla uyuşmuyor, hiçbir şey yazılmadı", entered.label())

	message := fmt.Sprintf(
		"%s için known_hosts dosyasında başka bir host key kayıtlı.\n\n"+
			"Sunucunun şu an sunduğu:\n%s\n\n"+
			"Sunucu yeniden kurulduysa known_hosts satırını elle silin. "+
			"Kurulmadıysa bağlantı beklediğiniz makineye gitmiyor demektir.\n\n"+
			"Hiçbir dosya değiştirilmedi.",
		entered.host, scan.Fingerprints())

	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord

	d := dialog.NewCustom("Host key değişmiş", "Kapat", container.NewScroll(body), a.window)
	d.Resize(fyne.NewSize(560, 340))
	d.Show()
}

// confirmHostKey shows the fingerprint before it is trusted. Accepting a host
// key without looking is the one step of an ssh setup that editing a file
// afterwards cannot undo.
func (a *App) confirmHostKey(entered serverForm, scan sshsetup.Scan, state sshsetup.KnownHostsState, onDone func()) {
	if state == sshsetup.HostMatches {
		// The key is already trusted, so there is nothing new to accept.
		a.writeServerSetup(entered, scan, false, onDone)
		return
	}

	message := fmt.Sprintf(
		"%s sunucusu aşağıdaki host key'i sunuyor:\n\n%s\n\n"+
			"Parmak izini sunucunun yöneticisinden aldığınızla karşılaştırın. "+
			"Kabul ederseniz known_hosts dosyasına eklenir.",
		entered.host, scan.Fingerprints())

	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord

	d := dialog.NewCustomConfirm("Host key onayı", "Kabul et", "Vazgeç",
		container.NewScroll(body), func(ok bool) {
			if !ok {
				a.log.addf("[%s] host key kabul edilmedi, hiçbir şey yazılmadı", entered.label())
				return
			}

			a.writeServerSetup(entered, scan, true, onDone)
		}, a.window)

	d.Resize(fyne.NewSize(560, 340))
	d.Show()
}

// writeServerSetup performs every write, then reloads and tests the result.
func (a *App) writeServerSetup(entered serverForm, scan sshsetup.Scan, trustHost bool, onDone func()) {
	var warnings []string

	a.run("Ayarlar yazılıyor", func(ctx context.Context) error {
		paths, err := sshsetup.Locate(a.config().SSH.ConfigFile)
		if err != nil {
			return err
		}

		key, err := sshsetup.WriteKey(ctx, paths.Dir, entered.label(), entered.key)
		if err != nil {
			return err
		}

		if key.Reused {
			a.log.addf("[%s] aynı içerikli anahtar zaten vardı, %s kullanılıyor", entered.label(), key.Path)
		} else {
			a.log.addf("[%s] anahtar yazıldı: %s", entered.label(), key.Path)
		}

		if warning := windowsKeyWarning(key.Path); warning != "" {
			warnings = append(warnings, warning)
		}

		if trustHost {
			if err := sshsetup.AppendKnownHosts(paths.KnownHosts, scan); err != nil {
				return err
			}

			a.log.addf("[%s] host key known_hosts dosyasına eklendi", entered.label())
		}

		if err := sshsetup.UpsertHostBlock(paths.Config, hostEntryFor(entered, key.Path)); err != nil {
			return err
		}

		a.log.addf("[%s] Host bloğu yazıldı: %s", entered.label(), paths.Config)

		if err := a.appendServerToConfig(entered); err != nil {
			return err
		}

		return nil
	}, func() {
		a.loadConfig()

		for _, warning := range warnings {
			dialog.ShowInformation("Dosya izinleri", warning, a.window)
		}

		if onDone != nil {
			onDone()
		}

		a.checkServer(entered)
	})
}

func hostEntryFor(entered serverForm, keyPath string) sshsetup.HostEntry {
	return sshsetup.HostEntry{
		Pattern:      entered.host,
		HostName:     entered.host,
		User:         entered.user,
		Port:         entered.port,
		IdentityFile: keyPath,
	}
}

// appendServerToConfig adds the server to the configuration file, creating it
// from the shipped example when there is none yet.
func (a *App) appendServerToConfig(entered serverForm) error {
	path, err := a.configTarget()
	if err != nil {
		return err
	}

	source, err := currentConfigSource(path)
	if err != nil {
		return err
	}

	updated, err := config.AddServer(source, entered.server())
	if err != nil {
		return err
	}

	// The same rule the settings editor follows: a file the application could
	// not load afterwards never reaches the disk.
	if _, err := config.Decode(updated); err != nil {
		return fmt.Errorf("ayar dosyası geçersiz olurdu, yazılmadı: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("dizin oluşturulamadı: %w", err)
	}

	if err := os.WriteFile(path, updated, configFileMode); err != nil {
		return fmt.Errorf("ayar dosyası yazılamadı (%s): %w", path, err)
	}

	a.log.addf("Ayar dosyası güncellendi: %s", path)

	return nil
}

// currentConfigSource returns the file to edit. A machine with no
// configuration starts from the shipped example with its sample servers
// removed, which carries the documentation of every other key into the new
// file without a second copy of the template existing.
func currentConfigSource(path string) ([]byte, error) {
	// The path is the configuration file the operator chose, either with
	// --config or through the search order.
	// #nosec G304
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("ayar dosyası okunamadı (%s): %w", path, err)
	}

	return config.ClearServers(config.Example)
}

// checkServer tests the server that was just added, so the user sees whether
// it works without having to go looking for the button that says so.
func (a *App) checkServer(entered serverForm) {
	cfg := a.config()

	var target config.Server

	for _, srv := range cfg.Servers {
		if srv.Host == entered.host {
			target = srv
		}
	}

	if target.Host == "" {
		return
	}

	a.run("Bağlantı deneniyor", func(ctx context.Context) error {
		st := unbound.Check(ctx, a.transportRunner(), target)

		if !st.Reachable {
			a.log.addf("[%s] bağlanılamadı: %v", st.Server.Label(), st.Err)
			return nil
		}

		a.log.addf("[%s] bağlantı tamam, %s, config %s, yenileme: %s",
			st.Server.Label(),
			boolText(st.ServiceActive, "servis aktif", "servis pasif"),
			boolText(st.ConfigValid, "geçerli", "GEÇERSİZ"),
			st.AvailableTier())

		return nil
	}, nil)
}

// windowsKeyWarning reports that the file mode Go just set is not what OpenSSH
// checks on Windows, where the permission comes from the ACL instead. Saying
// nothing would leave a key ssh refuses with a message that names none of this.
func windowsKeyWarning(path string) string {
	if runtime.GOOS != "windows" {
		return ""
	}

	return fmt.Sprintf(
		"Windows'ta OpenSSH anahtar izinlerini ACL üzerinden denetler.\n\n"+
			"Anahtar reddedilirse şu komutu çalıştırın:\n\n"+
			"icacls \"%s\" /inheritance:r /grant:r \"%%USERNAME%%:R\"",
		path)
}
