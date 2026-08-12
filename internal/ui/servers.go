//go:build !nogui

package ui

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/KilimcininKorOglu/yada/internal/unbound"
)

// buildServersTab shows connectivity and, for each server, which refresh tier
// it supports. Knowing the tier matters before writing: a server without
// remote-control loses its cache on every change, and one without ExecReload
// takes an outage.
func (a *App) buildServersTab() fyne.CanvasObject {
	statuses := []unbound.Status{}

	table := widget.NewTable(
		func() (int, int) { return len(statuses) + 1, 5 },
		func() fyne.CanvasObject { return widget.NewLabel("geniş içerik alanı") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			label.TextStyle = fyne.TextStyle{}

			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText([]string{"Sunucu", "Bağlantı", "Servis", "Config", "Yenileme"}[id.Col])

				return
			}

			st := statuses[id.Row-1]

			switch id.Col {
			case 0:
				label.SetText(st.Server.Label())
			case 1:
				label.SetText(boolText(st.Reachable, "tamam", "HATA"))
			case 2:
				label.SetText(stateText(st.Reachable, st.ServiceActive, "aktif", "PASİF"))
			case 3:
				label.SetText(stateText(st.Reachable, st.ConfigValid, "geçerli", "GEÇERSİZ"))
			case 4:
				label.SetText(st.AvailableTier())
			}
		},
	)

	for col, width := range []float32{160, 110, 100, 110, 200} {
		table.SetColumnWidth(col, width)
	}

	summary := widget.NewLabel("Durum için «Tümünü test et» düğmesine basın.")
	summary.Wrapping = fyne.TextWrapWord

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
				statuses = result
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
