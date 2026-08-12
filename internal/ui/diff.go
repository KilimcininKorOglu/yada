//go:build !nogui

package ui

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/diff"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/unbound"
)

func (a *App) buildDiffTab() fyne.CanvasObject {
	var entries []diff.Entry

	status := widget.NewLabel("Farkı görmek için «Karşılaştır» düğmesine basın.")
	status.Wrapping = fyne.TextWrapWord

	table := widget.NewTable(
		func() (int, int) { return len(entries) + 1, 4 },
		func() fyne.CanvasObject { return widget.NewLabel("geniş içerik alanı") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			label.TextStyle = fyne.TextStyle{}

			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText([]string{"Ad", "Tip", "Durum", "Ayrıntı"}[id.Col])

				return
			}

			entry := entries[id.Row-1]

			switch id.Col {
			case 0:
				label.SetText(strings.TrimSuffix(entry.Record.Name, "."))
			case 1:
				label.SetText(string(entry.Record.Type))
			case 2:
				label.SetText(entry.Status.String())
			case 3:
				label.SetText(entryDetail(entry))
			}
		},
	)

	for col, width := range []float32{240, 80, 100, 380} {
		table.SetColumnWidth(col, width)
	}

	sourceSelect := widget.NewSelect(nil, nil)
	pruneCheck := widget.NewCheck("Kaynakta olmayanları hedeflerden sil", nil)

	compare := func() {
		if !a.configured() {
			return
		}

		cfg := a.config()

		if len(cfg.Servers) < 2 {
			dialog.ShowInformation("Yetersiz sunucu",
				"Karşılaştırma için en az iki sunucu gerekir.", a.window)

			return
		}

		a.run("Sunucular karşılaştırılıyor", func(ctx context.Context) error {
			results := unbound.ReadAll(ctx, a.transportRunner(), cfg)

			var (
				sets   []diff.ServerSet
				failed []string
				labels []string
			)

			for _, res := range results {
				if res.Err != nil {
					failed = append(failed, res.Server.Label())
					a.log.addf("[%s] okunamadı: %v", res.Server.Label(), res.Err)

					continue
				}

				sets = append(sets, diff.ServerSet{Label: res.Server.Label(), Records: res.Records()})
				labels = append(labels, res.Server.Label())
			}

			// An unreadable server would otherwise look like one with no
			// records, which reads as "everything is missing here".
			if len(failed) > 0 {
				return fmt.Errorf("%s okunamadığı için karşılaştırma yapılamaz", strings.Join(failed, ", "))
			}

			comparison := diff.Compare(sets)

			var differing []diff.Entry
			for _, entry := range comparison.Entries {
				if entry.Status != diff.StatusSame {
					differing = append(differing, entry)
				}
			}

			fyne.Do(func() {
				entries = differing
				sourceSelect.Options = labels

				if sourceSelect.Selected == "" && len(labels) > 0 {
					sourceSelect.SetSelected(labels[0])
				}

				table.Refresh()

				if comparison.InSync() {
					status.SetText(fmt.Sprintf("Sunucular eşit (%d kayıt).", len(comparison.Entries)))
					return
				}

				status.SetText(fmt.Sprintf("%d fark, %d çakışma. Çakışmalar otomatik eşitlenmez.",
					len(differing), len(comparison.Conflicts())))
			})

			return nil
		}, nil)
	}

	syncButton := widget.NewButton("Eksikleri kopyala", func() {
		if sourceSelect.Selected == "" {
			dialog.ShowInformation("Kaynak seçilmedi", "Önce referans sunucuyu seçin.", a.window)
			return
		}

		a.runSync(sourceSelect.Selected, pruneCheck.Checked, compare)
	})

	toolbar := container.NewHBox(
		widget.NewButton("Karşılaştır", compare),
		widget.NewLabel("Kaynak:"),
		sourceSelect,
		syncButton,
	)

	header := container.NewVBox(toolbar, pruneCheck, widget.NewSeparator())

	return container.NewBorder(header, status, nil, nil, table)
}

func entryDetail(entry diff.Entry) string {
	switch entry.Status {
	case diff.StatusMissing:
		return fmt.Sprintf("%s sunucusunda yok", strings.Join(entry.Missing, ", "))
	case diff.StatusConflict:
		parts := make([]string, 0, len(entry.Present))
		for _, server := range entry.Present {
			parts = append(parts, fmt.Sprintf("%s=%s", server, entry.Values[server]))
		}

		return strings.Join(parts, "   ")
	default:
		return ""
	}
}

// runSync copies the source server's records onto the others. Conflicting
// records are left alone and listed, because picking a value is the user's
// call.
func (a *App) runSync(sourceLabel string, prune bool, onDone func()) {
	cfg := a.config()

	a.run("Sunucular eşitleniyor", func(ctx context.Context) error {
		results := unbound.ReadAll(ctx, a.transportRunner(), cfg)

		var (
			source  diff.ServerSet
			targets []diff.ServerSet
		)

		for _, res := range results {
			if res.Err != nil {
				return fmt.Errorf("%s okunamadı: %w", res.Server.Label(), res.Err)
			}

			set := diff.ServerSet{Label: res.Server.Label(), Records: res.Records()}

			if set.Label == sourceLabel {
				source = set
				continue
			}

			targets = append(targets, set)
		}

		if len(targets) == 0 {
			return fmt.Errorf("eşitlenecek hedef sunucu yok")
		}

		plans, conflicts := diff.PlanSync(source, targets, prune)

		for _, entry := range conflicts {
			a.log.addf("ÇAKIŞMA atlandı: %s %s (%s)",
				strings.TrimSuffix(entry.Record.Name, "."), entry.Record.Type, entryDetail(entry))
		}

		byServer := make(map[string]diff.Plan, len(plans))
		for _, plan := range plans {
			byServer[plan.Server] = plan
		}

		// The source must not be written to.
		scoped := cfg
		scoped.Servers = nil

		for _, srv := range cfg.Servers {
			if srv.Label() != sourceLabel {
				scoped.Servers = append(scoped.Servers, srv)
			}
		}

		opts := unbound.WriteOptions{Backup: cfg.Behaviour.BackupBeforeWrite}

		writeResults := unbound.ApplyPerServer(ctx, a.transportRunner(), scoped, opts,
			func(srv config.Server, f *records.File) error {
				plan := byServer[srv.Label()]

				for _, rec := range plan.Add {
					if err := f.Add(rec); err != nil {
						return err
					}
				}

				for _, rec := range plan.Remove {
					f.Delete(rec.Name, rec.Type, "")
				}

				if prune && len(plan.Remove) > 0 {
					f.PruneUnusedZones()
				}

				return nil
			})

		a.reportWrites(writeResults)

		return a.refresh(ctx, cfg, writeResults)
	}, onDone)
}
