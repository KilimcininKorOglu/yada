package records

import (
	"strings"
	"testing"
)

func parseFile(t *testing.T, body string) *File {
	t.Helper()

	f, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("dosya ayrıştırılamadı: %v", err)
	}

	return f
}

func keys(recs []Record) []string {
	out := make([]string, len(recs))
	for i, rec := range recs {
		out[i] = rec.FullKey()
	}

	return out
}

func TestDiffReportsAddedAndRemoved(t *testing.T) {
	before := parseFile(t, `
		local-zone: "google.com." transparent
		local-data: "mail.google.com. IN A 10.10.10.10"
`)
	after := parseFile(t, `
		local-zone: "google.com." transparent
		local-data: "mail.google.com. IN A 10.10.10.10"
		local-data: "web.google.com. IN A 10.20.20.20"
`)

	change := Diff(before, after)

	if len(change.Added) != 1 || change.Added[0].Name != "web.google.com." {
		t.Errorf("eklenenler = %v, yalnızca web.google.com. olmalı", keys(change.Added))
	}
	if len(change.Removed) != 0 {
		t.Errorf("silinenler = %v, boş olmalı", keys(change.Removed))
	}
	if change.Empty() {
		t.Error("değişiklik boş bildirildi")
	}
}

// A changed value is a removal plus an addition, which is exactly what the
// runtime push has to do: drop the old data, install the new.
func TestDiffTreatsChangedValueAsBothSides(t *testing.T) {
	before := parseFile(t, "local-data: \"mail.google.com. IN A 10.10.10.10\"\n")
	after := parseFile(t, "local-data: \"mail.google.com. IN A 10.20.30.40\"\n")

	change := Diff(before, after)

	if len(change.Added) != 1 || change.Added[0].Value != "10.20.30.40" {
		t.Errorf("eklenenler = %v, yeni değer olmalı", keys(change.Added))
	}
	if len(change.Removed) != 1 || change.Removed[0].Value != "10.10.10.10" {
		t.Errorf("silinenler = %v, eski değer olmalı", keys(change.Removed))
	}
}

// unbound-control local_data_remove wipes every type under a name, so the
// records that survive have to travel with the change or the daemon would lose
// them while the file still holds them.
func TestDiffCollectsRetainedRecordsOfARemovedName(t *testing.T) {
	before := parseFile(t, `
		local-data: "mail.google.com. IN A 10.10.10.10"
		local-data: "mail.google.com. IN TXT \"v=spf1 -all\""
		local-data: "web.google.com. IN A 10.20.20.20"
`)
	after := parseFile(t, `
		local-data: "mail.google.com. IN TXT \"v=spf1 -all\""
		local-data: "web.google.com. IN A 10.20.20.20"
`)

	change := Diff(before, after)

	if len(change.Retained) != 1 {
		t.Fatalf("korunanlar = %v, yalnızca TXT olmalı", keys(change.Retained))
	}
	if change.Retained[0].Type != TypeTXT {
		t.Errorf("korunan kayıt tipi = %q, TXT olmalı", change.Retained[0].Type)
	}

	// A record under an untouched name is not at risk and must not be resent.
	for _, rec := range change.Retained {
		if rec.Name == "web.google.com." {
			t.Error("silinmeyen ada ait kayıt gereksiz yere korunanlara alındı")
		}
	}
}

func TestDiffReportsRemovedZones(t *testing.T) {
	before := parseFile(t, `
		local-zone: "google.com." transparent
		local-data: "mail.google.com. IN A 10.10.10.10"
`)
	after := parseFile(t, "")

	change := Diff(before, after)

	if len(change.ZonesRemoved) != 1 || change.ZonesRemoved[0] != "google.com." {
		t.Errorf("kaldırılan zone'lar = %v, google.com. olmalı", change.ZonesRemoved)
	}
}

func TestDiffOfIdenticalFilesIsEmpty(t *testing.T) {
	body := "local-zone: \"google.com.\" transparent\nlocal-data: \"mail.google.com. IN A 10.10.10.10\"\n"

	change := Diff(parseFile(t, body), parseFile(t, body))

	if !change.Empty() {
		t.Errorf("aynı dosyalar için değişiklik bulundu: %+v", change)
	}
}

func TestParseWireLineSkipsNoise(t *testing.T) {
	for _, line := range []string{"", "   ", "; yorum", "bozuk"} {
		if _, ok := ParseWireLine(line); ok {
			t.Errorf("%q kayıt olarak ayrıştırıldı", line)
		}
	}

	rec, ok := ParseWireLine("mail.google.com.\t3600\tIN\tA\t10.10.10.10")
	if !ok {
		t.Fatal("geçerli satır ayrıştırılamadı")
	}

	if rec.Name != "mail.google.com." || rec.Type != TypeA || rec.Value != "10.10.10.10" {
		t.Errorf("satır yanlış ayrıştırıldı: %+v", rec)
	}
}

// The TXT value keeps the quotes the daemon printed, so rendering it back does
// not add a second pair.
func TestParseWireLineKeepsQuotedTXTIntact(t *testing.T) {
	rec, ok := ParseWireLine(`mail.google.com.	IN	TXT	"v=spf1 -all"`)
	if !ok {
		t.Fatal("TXT satırı ayrıştırılamadı")
	}

	if got := rec.String(); strings.Count(got, `"`) != 2 {
		t.Errorf("TXT tekrar tırnaklandı: %s", got)
	}
}
