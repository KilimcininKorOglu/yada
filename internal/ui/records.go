//go:build !nogui

package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/KilimcininKorOglu/yada/internal/records"
	"github.com/KilimcininKorOglu/yada/internal/unbound"
)

// writeMode says how a record reaches the servers, which the form and the
// write both need to know.
type writeMode int

const (
	// writeAdd is a new record: the name and type are still editable, and the
	// servers are checked before anything is written.
	writeAdd writeMode = iota

	// writeUpdate changes a record the user picked from the table.
	writeUpdate

	// writeSet adds the record where it is missing and replaces it where it
	// differs, which is how one decision reaches servers that disagree.
	writeSet
)

// recordRow is a record together with the servers that hold it.
type recordRow struct {
	record    records.Record
	servers   []string
	conflicts map[string]string
}

func (r recordRow) conflicted() bool {
	return len(r.conflicts) > 0
}

func (a *App) buildRecordsTab() fyne.CanvasObject {
	var (
		all      []recordRow
		shown    []recordRow
		selected = -1
	)

	filter := widget.NewEntry()
	filter.SetPlaceHolder("Ada göre süz")

	typeFilter := widget.NewSelect(append([]string{"Tümü"}, typeNames()...), nil)
	typeFilter.SetSelected("Tümü")

	status := widget.NewLabel("Kayıtları görmek için «Yenile» düğmesine basın.")

	table := widget.NewTable(
		func() (int, int) { return len(shown) + 1, 5 },
		func() fyne.CanvasObject { return widget.NewLabel("geniş içerik alanı") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			label.TextStyle = fyne.TextStyle{}

			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText([]string{"Ad", "Tip", "Değer", "TTL", "Sunucular"}[id.Col])

				return
			}

			row := shown[id.Row-1]

			switch id.Col {
			case 0:
				label.SetText(strings.TrimSuffix(row.record.Name, "."))
			case 1:
				label.SetText(string(row.record.Type))
			case 2:
				value := row.record.Value
				if row.conflicted() {
					value += "  (ÇAKIŞMA)"
				}
				label.SetText(value)
			case 3:
				label.SetText(ttlText(row.record.TTL))
			case 4:
				label.SetText(strings.Join(row.servers, ", "))
			}
		},
	)

	for col, width := range []float32{240, 80, 280, 70, 180} {
		table.SetColumnWidth(col, width)
	}

	table.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			selected = -1
			return
		}

		selected = id.Row - 1
	}

	applyFilter := func() {
		needle := strings.ToLower(strings.TrimSpace(filter.Text))
		wantType := typeFilter.Selected

		shown = shown[:0]

		for _, row := range all {
			if needle != "" && !strings.Contains(strings.ToLower(row.record.Name), needle) {
				continue
			}
			if wantType != "" && wantType != "Tümü" && string(row.record.Type) != wantType {
				continue
			}

			shown = append(shown, row)
		}

		table.Refresh()
		status.SetText(fmt.Sprintf("%d kayıt gösteriliyor (toplam %d).", len(shown), len(all)))
	}

	filter.OnChanged = func(string) { applyFilter() }
	typeFilter.OnChanged = func(string) { applyFilter() }

	reload := func() {
		if !a.configured() {
			return
		}

		a.run("Kayıtlar okunuyor", func(ctx context.Context) error {
			results := unbound.ReadAll(ctx, a.transportRunner(), a.config())

			rows := collectRecordRows(results)

			for _, res := range results {
				if res.Err != nil {
					a.log.addf("[%s] okunamadı: %v", res.Server.Label(), res.Err)
				}
			}

			fyne.Do(func() {
				all = rows
				selected = -1
				applyFilter()
			})

			return nil
		}, nil)
	}

	refreshButton := widget.NewButton("Yenile", reload)

	addButton := widget.NewButton("Ekle", func() {
		if !a.configured() {
			return
		}

		a.showRecordDialog(records.Record{}, writeAdd, reload)
	})

	editButton := widget.NewButton("Düzenle", func() {
		if selected < 0 || selected >= len(shown) {
			dialog.ShowInformation("Seçim yok", "Önce tablodan bir kayıt seçin.", a.window)
			return
		}

		a.showRecordDialog(shown[selected].record, writeUpdate, reload)
	})

	deleteButton := widget.NewButton("Sil", func() {
		if selected < 0 || selected >= len(shown) {
			dialog.ShowInformation("Seçim yok", "Önce tablodan bir kayıt seçin.", a.window)
			return
		}

		row := shown[selected]

		a.confirmDestructive(
			"Kaydı sil",
			fmt.Sprintf("%s %s kaydı %s sunucusundan silinecek.",
				strings.TrimSuffix(row.record.Name, "."), row.record.Type, strings.Join(row.servers, ", ")),
			1,
			func() { a.deleteRecord(row.record, reload) },
		)
	})

	toolbar := container.NewHBox(refreshButton, addButton, editButton, deleteButton)
	filters := container.NewBorder(nil, nil, widget.NewLabel("Tip:"), nil,
		container.NewBorder(nil, nil, nil, typeFilter, filter))

	header := container.NewVBox(toolbar, filters, widget.NewSeparator())

	return container.NewBorder(header, status, nil, nil, table)
}

// collectRecordRows merges the per-server records, flagging any name and type
// the servers disagree about instead of letting one value stand for all.
func collectRecordRows(results []unbound.ServerRecords) []recordRow {
	byKey := make(map[string]*recordRow)
	var order []string

	for _, res := range results {
		for _, rec := range res.Records() {
			key := rec.Key()

			existing, seen := byKey[key]
			if !seen {
				byKey[key] = &recordRow{
					record:    rec,
					servers:   []string{res.Server.Label()},
					conflicts: map[string]string{},
				}
				order = append(order, key)

				continue
			}

			existing.servers = append(existing.servers, res.Server.Label())

			if rec.Value != existing.record.Value {
				existing.conflicts[res.Server.Label()] = rec.Value
			}
		}
	}

	sort.Strings(order)

	rows := make([]recordRow, 0, len(order))
	for _, key := range order {
		rows = append(rows, *byKey[key])
	}

	return rows
}

// showRecordDialog builds the add and edit form. The value field is validated
// per type as the user types, so a mistake surfaces before anything is sent.
func (a *App) showRecordDialog(existing records.Record, mode writeMode, onDone func()) {
	// Only a new record may choose its name and type; changing either on an
	// existing one would make it a different record.
	editing := mode != writeAdd

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("mail.example.com")

	typeSelect := widget.NewSelect(typeNames(), nil)
	typeSelect.SetSelected(string(records.TypeA))

	valueEntry := widget.NewEntry()
	ttlEntry := widget.NewEntry()
	ttlEntry.SetPlaceHolder("boş bırakılırsa Unbound varsayılanı")

	if editing {
		nameEntry.SetText(strings.TrimSuffix(existing.Name, "."))
		nameEntry.Disable()
		typeSelect.SetSelected(string(existing.Type))
		typeSelect.Disable()
		valueEntry.SetText(existing.Value)

		if existing.TTL != nil {
			ttlEntry.SetText(strconv.FormatUint(uint64(*existing.TTL), 10))
		}
	}

	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	validate := func() error {
		recType, err := records.ParseType(typeSelect.Selected)
		if err != nil {
			return err
		}

		ttl, err := parseTTL(ttlEntry.Text)
		if err != nil {
			return err
		}

		_, err = records.New(nameEntry.Text, recType, valueEntry.Text, ttl)

		return err
	}

	showValidation := func(string) {
		if err := validate(); err != nil {
			errorLabel.SetText(err.Error())
			return
		}

		errorLabel.SetText("")
	}

	valueEntry.OnChanged = showValidation
	nameEntry.OnChanged = showValidation
	ttlEntry.OnChanged = showValidation
	typeSelect.OnChanged = func(string) { showValidation("") }

	form := widget.NewForm(
		widget.NewFormItem("Ad", nameEntry),
		widget.NewFormItem("Tip", typeSelect),
		widget.NewFormItem("Değer", valueEntry),
		widget.NewFormItem("TTL", ttlEntry),
	)

	content := container.NewVBox(form, errorLabel)

	title := "Kayıt ekle"
	if editing {
		title = "Kaydı düzenle"
	}

	d := dialog.NewCustomConfirm(title, "Kaydet", "Vazgeç", content, func(ok bool) {
		if !ok {
			return
		}

		if err := validate(); err != nil {
			dialog.ShowError(err, a.window)
			return
		}

		recType, _ := records.ParseType(typeSelect.Selected)
		ttl, _ := parseTTL(ttlEntry.Text)

		rec, err := records.New(nameEntry.Text, recType, valueEntry.Text, ttl)
		if err != nil {
			dialog.ShowError(err, a.window)
			return
		}

		// A new record is checked against the servers first, so an existing
		// name is reported rather than silently rejected by the write.
		if mode == writeAdd {
			a.startAdd(rec, onDone)
			return
		}

		a.writeRecord(rec, mode, onDone)
	}, a.window)

	d.Resize(fyne.NewSize(520, 320))
	d.Show()
}

// startAdd reads the servers before writing, so the user learns that a name is
// taken while it is still their decision what to do about it.
//
// The check and the write are two separate a.run calls rather than one: a.run
// keeps a progress dialog up for as long as its work runs, so a question can
// only be asked once that work has ended.
func (a *App) startAdd(rec records.Record, onDone func()) {
	var (
		check   unbound.AddCheck
		checked bool
	)

	a.run("Kayıt denetleniyor", func(ctx context.Context) error {
		check = unbound.CheckAdd(ctx, a.transportRunner(), a.config(), rec)

		for _, state := range check.States {
			if state.Err != nil {
				a.log.addf("[%s] okunamadı: %v", state.Server.Label(), state.Err)
			}
		}

		if check.Readable == 0 {
			return errors.New("hiçbir sunucu okunamadı")
		}

		checked = true

		return nil
	}, func() {
		if !checked {
			if onDone != nil {
				onDone()
			}

			return
		}

		switch check.Outcome {
		case unbound.AddDuplicate:
			a.log.addf("%s kaydı zaten var, değişiklik yapılmadı.", rec.String())
			dialog.ShowInformation("Kayıt zaten var",
				"Bu kayıt sunucularda aynı değerle duruyor.\n\n"+check.Summary(), a.window)

			if onDone != nil {
				onDone()
			}

		case unbound.AddConflict:
			a.confirmOverwrite(rec, check, onDone)

		default:
			a.writeRecord(rec, writeSet, onDone)
		}
	})
}

// confirmOverwrite offers the two ways out of a name that is already taken:
// open the record for editing, or leave the servers alone.
func (a *App) confirmOverwrite(rec records.Record, check unbound.AddCheck, onDone func()) {
	message := fmt.Sprintf("%s için %s kaydı başka bir değerle duruyor.\n\n%s\n\nDüzenlerseniz kayıt %s "+
		"değeriyle her sunucuda aynı hale getirilir.",
		strings.TrimSuffix(rec.Name, "."), rec.Type, check.Summary(), rec.Value)

	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord

	d := dialog.NewCustomConfirm("Kayıt zaten var", "Düzenle", "Vazgeç",
		container.NewScroll(body), func(ok bool) {
			if !ok {
				a.log.addf("%s kaydı zaten var, işlem iptal edildi.", strings.TrimSuffix(rec.Name, "."))
				return
			}

			a.showRecordDialog(rec, writeSet, onDone)
		}, a.window)

	d.Resize(fyne.NewSize(520, 320))
	d.Show()
}

// writeRecord writes a record to every server, then refreshes the ones that
// actually changed.
func (a *App) writeRecord(rec records.Record, mode writeMode, onDone func()) {
	cfg := a.config()

	opts := unbound.WriteOptions{Backup: cfg.Behaviour.BackupBeforeWrite}

	a.run("Kayıt yazılıyor", func(ctx context.Context) error {
		results := unbound.Apply(ctx, a.transportRunner(), cfg, opts, func(f *records.File) error {
			switch mode {
			case writeUpdate:
				return f.Update(rec)
			case writeSet:
				return f.Set(rec)
			default:
				return f.Add(rec)
			}
		})

		a.reportWrites(results)

		return a.refresh(ctx, cfg, results)
	}, onDone)
}

func (a *App) deleteRecord(rec records.Record, onDone func()) {
	cfg := a.config()

	opts := unbound.WriteOptions{Backup: cfg.Behaviour.BackupBeforeWrite}

	a.run("Kayıt siliniyor", func(ctx context.Context) error {
		results := unbound.Apply(ctx, a.transportRunner(), cfg, opts, func(f *records.File) error {
			if f.Delete(rec.Name, rec.Type, "") > 0 {
				f.PruneUnusedZones()
			}

			return nil
		})

		a.reportWrites(results)

		return a.refresh(ctx, cfg, results)
	}, onDone)
}

// reportWrites logs each server's outcome.
func (a *App) reportWrites(results []unbound.WriteResult) {
	for _, res := range results {
		label := res.Server.Label()

		switch {
		case res.Err != nil:
			a.log.addf("[%s] BAŞARISIZ: %v", label, res.Err)

			if res.RolledBack {
				a.log.addf("[%s] dosya yedekten geri yüklendi", label)
			}
			if res.CheckOutput != "" {
				a.log.addf("[%s] checkconf: %s", label, res.CheckOutput)
			}

		case res.Diff.Empty():
			a.log.addf("[%s] değişiklik yok", label)

		default:
			a.log.addf("[%s] yazıldı:\n%s", label, strings.TrimRight(res.Diff.String(), "\n"))
		}
	}
}

// refresh makes a write take effect on the servers it changed, pushing each
// one's own record change into its daemon.
func (a *App) refresh(ctx context.Context, cfg config.Config, results []unbound.WriteResult) error {
	for _, res := range unbound.RefreshWrites(ctx, a.transportRunner(), cfg, results) {
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
}

func typeNames() []string {
	names := make([]string, len(records.SupportedTypes))
	for i, t := range records.SupportedTypes {
		names[i] = string(t)
	}

	return names
}

func parseTTL(text string) (*uint32, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := strconv.ParseUint(trimmed, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("TTL %q geçersiz, saniye cinsinden bir sayı olmalı", text)
	}

	return new(uint32(parsed)), nil
}

func ttlText(ttl *uint32) string {
	if ttl == nil {
		return "-"
	}

	return strconv.FormatUint(uint64(*ttl), 10)
}
