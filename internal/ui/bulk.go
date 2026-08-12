package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/kerem/unbound-dns/internal/bulk"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/unbound"
)

func (a *App) buildBulkTab() fyne.CanvasObject {
	var parsed bulk.ImportResult

	preview := widget.NewMultiLineEntry()
	preview.Wrapping = fyne.TextWrapOff
	preview.SetPlaceHolder("CSV dosyası seçildiğinde önizleme burada görünür.")

	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	applyButton := widget.NewButton("Geçerli satırları uygula", nil)
	applyButton.Disable()

	replaceCheck := widget.NewCheck("Mevcut kayıtların yerine geç (dosyada olmayanlar silinir)", nil)

	chooseButton := widget.NewButton("CSV seç", func() {
		open := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			if reader == nil {
				return
			}
			defer func() { _ = reader.Close() }()

			result, err := bulk.Import(reader)
			if err != nil {
				dialog.ShowError(err, a.window)
				return
			}

			parsed = result

			preview.SetText(previewText(result))
			status.SetText(fmt.Sprintf("%d geçerli satır, %d hatalı satır.",
				len(result.Records), len(result.Errors)))

			if len(result.Records) > 0 {
				applyButton.Enable()
			} else {
				applyButton.Disable()
			}
		}, a.window)

		open.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
		open.Show()
	})

	applyButton.OnTapped = func() {
		if !a.configured() || len(parsed.Records) == 0 {
			return
		}

		apply := func() { a.applyImport(parsed.Records, replaceCheck.Checked) }

		if replaceCheck.Checked {
			a.confirmDestructive(
				"Kayıtların yerine geç",
				"Dosyada olmayan kayıtlar sunuculardan silinecek.",
				len(parsed.Records),
				apply,
			)

			return
		}

		apply()
	}

	exportButton := widget.NewButton("Kayıtları CSV olarak kaydet", func() {
		if !a.configured() {
			return
		}

		save := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, a.window)
				return
			}
			if writer == nil {
				return
			}

			a.exportRecords(writer)
		}, a.window)

		save.SetFileName("unbound-kayitlar.csv")
		save.Show()
	})

	toolbar := container.NewHBox(chooseButton, applyButton, exportButton)
	header := container.NewVBox(toolbar, replaceCheck, widget.NewSeparator())

	return container.NewBorder(header, status, nil, nil, preview)
}

// previewText lists the valid rows and the rejected ones with their line
// numbers, so a typo is visible before anything is written.
func previewText(result bulk.ImportResult) string {
	var b strings.Builder

	if len(result.Errors) > 0 {
		fmt.Fprintf(&b, "ATLANACAK SATIRLAR (%d):\n", len(result.Errors))

		for _, rowErr := range result.Errors {
			fmt.Fprintf(&b, "  %s\n", rowErr)
		}

		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "UYGULANACAK KAYITLAR (%d):\n", len(result.Records))

	for _, rec := range result.Records {
		fmt.Fprintf(&b, "  %s\n", rec.String())
	}

	return b.String()
}

func (a *App) applyImport(recs []records.Record, replace bool) {
	cfg := a.config()
	opts := unbound.WriteOptions{Backup: cfg.Behaviour.BackupBeforeWrite}

	a.run("Kayıtlar uygulanıyor", func(ctx context.Context) error {
		results := unbound.Apply(ctx, a.transportRunner(), cfg, opts, func(f *records.File) error {
			if replace {
				for _, existing := range f.All() {
					f.Delete(existing.Name, existing.Type, "")
				}
			}

			for _, rec := range recs {
				if err := f.Add(rec); err != nil {
					// An existing record becomes an update, so re-applying a
					// file converges instead of failing on every row.
					var exists *records.ErrExists
					if errors.As(err, &exists) {
						if err := f.Update(rec); err != nil {
							return err
						}

						continue
					}

					return err
				}
			}

			if replace {
				f.PruneUnusedZones()
			}

			return nil
		})

		changed := a.reportWrites(results)

		return a.refresh(ctx, cfg, changed)
	}, nil)
}

func (a *App) exportRecords(writer fyne.URIWriteCloser) {
	cfg := a.config()

	a.run("Kayıtlar dışa aktarılıyor", func(ctx context.Context) error {
		results := unbound.ReadAll(ctx, a.transportRunner(), cfg)

		seen := make(map[string]records.Record)
		var order []string

		for _, res := range results {
			if res.Err != nil {
				a.log.addf("[%s] okunamadı: %v", res.Server.Label(), res.Err)
				continue
			}

			for _, rec := range res.Records() {
				if _, exists := seen[rec.Key()]; !exists {
					seen[rec.Key()] = rec
					order = append(order, rec.Key())
				}
			}
		}

		recs := make([]records.Record, 0, len(order))
		for _, key := range order {
			recs = append(recs, seen[key])
		}

		if err := writeExport(writer, recs); err != nil {
			return err
		}

		a.log.addf("%d kayıt %s dosyasına yazıldı", len(recs), writer.URI().Path())

		return nil
	}, nil)
}

// writeExport closes the writer explicitly and reports a close failure,
// because a failed flush leaves a short file on disk.
func writeExport(writer io.WriteCloser, recs []records.Record) error {
	if err := bulk.Export(writer, recs); err != nil {
		_ = writer.Close()
		return err
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("dosya kapatılamadı, içerik eksik olabilir: %w", err)
	}

	return nil
}
