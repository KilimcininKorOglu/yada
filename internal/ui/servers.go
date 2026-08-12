//go:build !nogui

package ui

import (
	"context"
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/KilimcininKorOglu/yada/internal/unbound"
)

// buildServersTab shows connectivity and, for each server, which refresh tier
// it supports. Knowing the tier matters before writing: a server without
// remote-control loses its cache on every change, and one without ExecReload
// takes an outage.
func (a *App) buildServersTab() fyne.CanvasObject {
	// The rows come from the configuration, not from a test. Which servers are
	// configured is known without touching the network, so the table has
	// something to show the moment the tab opens.
	rows := serverRows(a.config(), nil)

	table := widget.NewTable(
		func() (int, int) { return len(rows) + 1, len(serverColumns) },
		func() fyne.CanvasObject { return widget.NewLabel("geniş içerik alanı") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			label.TextStyle = fyne.TextStyle{}

			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(serverColumns[id.Col])

				return
			}

			label.SetText(rows[id.Row-1].cell(id.Col))
		},
	)

	for col, width := range []float32{140, 200, 110, 100, 110, 190} {
		table.SetColumnWidth(col, width)
	}

	summary := widget.NewLabel("")
	summary.Wrapping = fyne.TextWrapWord

	// reseed redraws the table from the configuration alone, so a server added
	// or edited shows up even before it has been reached.
	reseed := func() {
		rows = serverRows(a.config(), nil)
		table.Refresh()
		summary.SetText(describeServers(a.config()))
	}

	reseed()
	a.onConfigChange(reseed)

	// Declared before the buttons so each of them can ask for a fresh test
	// after it changes something.
	var refresh *widget.Button

	addServer := widget.NewButton("Sunucu ekle", func() {
		a.showAddServerDialog(func() { refresh.OnTapped() })
	})

	refresh = widget.NewButton("Tümünü test et", func() {
		if !a.configured() {
			return
		}

		a.run("Sunucular test ediliyor", func(ctx context.Context) error {
			result := unbound.CheckAll(ctx, a.transportRunner(), a.config())

			fyne.Do(func() {
				rows = serverRows(a.config(), result)
				table.Refresh()
				summary.SetText(summarise(result))
			})

			for _, st := range result {
				if st.Err != nil {
					a.log.addf("[%s] bağlanılamadı: %v", st.Server.Label(), st.Err)
					continue
				}

				a.log.addf("[%s] %s, config %s, yenileme: %s",
					st.Server.Label(),
					boolText(st.ServiceActive, "servis aktif", "servis pasif"),
					boolText(st.ConfigValid, "geçerli", "GEÇERSİZ"),
					st.AvailableTier())
			}

			return nil
		}, nil)
	})

	reloadAll := widget.NewButton("Servisleri yenile", func() {
		if !a.configured() {
			return
		}

		a.run("Servisler yenileniyor", func(ctx context.Context) error {
			results := unbound.ReloadAll(ctx, a.transportRunner(), a.config())

			for _, res := range results {
				for _, attempt := range res.Attempts {
					if attempt.Err != nil {
						a.log.addf("[%s] %s kullanılamadı: %v", res.Server.Label(), attempt.Tier, attempt.Err)
					}
				}

				if res.Err != nil {
					a.log.addf("[%s] yenileme başarısız: %v", res.Server.Label(), res.Err)
					continue
				}

				a.log.addf("[%s] %s ile yenilendi (%s)", res.Server.Label(), res.Tier, res.Tier.Description())
			}

			return nil
		}, nil)
	})

	toolbar := container.NewHBox(addServer, refresh, reloadAll)

	return container.NewBorder(
		container.NewVBox(toolbar, widget.NewSeparator()),
		summary, nil, nil,
		table,
	)
}

var serverColumns = []string{"Sunucu", "Adres", "Bağlantı", "Servis", "Config", "Yenileme"}

// serverRow is a configured server with the result of the last test, when
// there has been one.
type serverRow struct {
	server config.Server

	// status is nil until the server has been tested, which is why the state
	// columns can say so instead of showing a failure that never happened.
	status *unbound.Status
}

// cell renders one column.
func (r serverRow) cell(col int) string {
	if r.status == nil {
		switch col {
		case 0:
			return r.server.Label()
		case 1:
			return serverAddress(r.server)
		default:
			return "denenmedi"
		}
	}

	st := *r.status

	switch col {
	case 0:
		return st.Server.Label()
	case 1:
		return serverAddress(st.Server)
	case 2:
		return boolText(st.Reachable, "tamam", "HATA")
	case 3:
		return stateText(st.Reachable, st.ServiceActive, "aktif", "PASİF")
	case 4:
		return stateText(st.Reachable, st.ConfigValid, "geçerli", "GEÇERSİZ")
	default:
		return st.AvailableTier()
	}
}

// serverAddress renders how the server is reached, which is what tells two
// entries on the same host apart.
func serverAddress(srv config.Server) string {
	address := srv.Host
	if srv.User != "" {
		address = srv.User + "@" + address
	}

	if srv.Port != 0 {
		address += ":" + strconv.Itoa(srv.Port)
	}

	return address
}

// serverRows pairs the configured servers with the statuses of the last test.
// A status is matched by label, so a server added since the test simply has
// none.
func serverRows(cfg config.Config, statuses []unbound.Status) []serverRow {
	byLabel := make(map[string]*unbound.Status, len(statuses))

	for i := range statuses {
		byLabel[statuses[i].Server.Label()] = &statuses[i]
	}

	rows := make([]serverRow, 0, len(cfg.Servers))

	for _, srv := range cfg.Servers {
		rows = append(rows, serverRow{server: srv, status: byLabel[srv.Label()]})
	}

	return rows
}

// serverLabels lists the configured servers, for the pickers that choose one.
// Which servers exist is not a question for the network, so a picker can be
// filled before anything has been read.
func serverLabels(cfg config.Config) []string {
	labels := make([]string, 0, len(cfg.Servers))

	for _, srv := range cfg.Servers {
		labels = append(labels, srv.Label())
	}

	return labels
}

// describeServers is the summary shown before any test has run.
func describeServers(cfg config.Config) string {
	if len(cfg.Servers) == 0 {
		return "Tanımlı sunucu yok. «Sunucu ekle» ile başlayın."
	}

	return fmt.Sprintf("%d sunucu tanımlı. Durum için «Tümünü test et» düğmesine basın.", len(cfg.Servers))
}

func summarise(statuses []unbound.Status) string {
	reachable, valid := 0, 0

	for _, st := range statuses {
		if st.Reachable {
			reachable++
		}
		if st.ConfigValid {
			valid++
		}
	}

	return fmt.Sprintf("%d sunucudan %d tanesine ulaşıldı, %d tanesinin config'i geçerli.",
		len(statuses), reachable, valid)
}

func boolText(ok bool, yes, no string) string {
	if ok {
		return yes
	}

	return no
}

// stateText renders a field that is only meaningful when the server answered.
func stateText(reachable, ok bool, yes, no string) string {
	if !reachable {
		return "-"
	}

	return boolText(ok, yes, no)
}
