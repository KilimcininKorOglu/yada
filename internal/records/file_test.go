package records

import (
	"strings"
	"testing"
)

// existingFile mixes generated lines with comments, blank lines and an
// unrelated directive, which is what a real file looks like after an operator
// has edited it by hand.
const existingFile = `# Yerel DNS kayıtları
# Bu dosya unbound.conf içinden include ile çağrılır.

         local-zone: "google.com." transparent
         local-data: "mail.google.com. IN A 10.10.10.10"
         local-data: "www.google.com. IN A 10.30.30.30"

# Aşağıdaki kayıt geçicidir, 2026 sonunda kaldırılacak
         local-data: "eski.google.com. IN A 10.40.40.40"

         local-zone: "internal.local." static
         local-data: "db.internal.local. 3600 IN A 192.168.1.10"
         local-data: "web.internal.local. IN CNAME db.internal.local."
`

func TestParseRoundTripPreservesBytes(t *testing.T) {
	f, err := Parse([]byte(existingFile))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	if got := string(f.Bytes()); got != existingFile {
		t.Errorf("round-trip dosyayı değiştirdi:\n--- gelen ---\n%s\n--- beklenen ---\n%s", got, existingFile)
	}
}

func TestParseReadsRecords(t *testing.T) {
	f, err := Parse([]byte(existingFile))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	all := f.All()
	if len(all) != 5 {
		t.Fatalf("%d kayıt bulundu, beklenen 5: %+v", len(all), all)
	}

	first := all[0]
	if first.Name != "mail.google.com." {
		t.Errorf("ad = %q", first.Name)
	}
	if first.Type != TypeA {
		t.Errorf("tip = %q", first.Type)
	}
	if first.Value != "10.10.10.10" {
		t.Errorf("değer = %q", first.Value)
	}
	if first.TTL != nil {
		t.Errorf("TTL = %v, verilmemişti", *first.TTL)
	}
}

func TestParseReadsTTL(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	got := f.Find("db.internal.local", TypeA)
	if len(got) != 1 {
		t.Fatalf("%d kayıt bulundu", len(got))
	}

	if got[0].TTL == nil {
		t.Fatal("TTL okunmadı")
	}
	if *got[0].TTL != 3600 {
		t.Errorf("TTL = %d, beklenen 3600", *got[0].TTL)
	}
}

func TestParseReadsZones(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	zones := f.Zones()

	if zones["google.com."] != "transparent" {
		t.Errorf("google.com. zone tipi = %q", zones["google.com."])
	}
	if zones["internal.local."] != "static" {
		t.Errorf("internal.local. zone tipi = %q", zones["internal.local."])
	}
}

func TestAddKeepsExistingLinesIntact(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, err := New("yeni.google.com", TypeA, "10.50.50.50", nil)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}

	out := string(f.Bytes())

	// Everything that was there before must still be there, comments included.
	if !strings.HasPrefix(out, existingFile) {
		t.Errorf("mevcut içerik korunmadı:\n%s", out)
	}
	if !strings.Contains(out, `         local-data: "yeni.google.com. IN A 10.50.50.50"`) {
		t.Errorf("yeni kayıt beklenen biçimde yazılmadı:\n%s", out)
	}
	// google.com. is already declared, so no second zone line may appear.
	if strings.Count(out, `local-zone: "google.com."`) != 1 {
		t.Errorf("var olan zone tekrar eklendi:\n%s", out)
	}
}

func TestAddCreatesMissingZone(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, _ := New("host.yenidomain.com", TypeA, "10.60.60.60", nil)
	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}

	out := string(f.Bytes())

	if !strings.Contains(out, `         local-zone: "yenidomain.com." transparent`) {
		t.Errorf("eksik zone eklenmedi:\n%s", out)
	}
}

func TestAddRejectsDuplicate(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, _ := New("mail.google.com", TypeA, "10.99.99.99", nil)

	err := f.Add(rec)
	if err == nil {
		t.Fatal("aynı ad ve tip için ikinci kayıt kabul edildi")
	}

	var exists *ErrExists
	if !asErrExists(err, &exists) {
		t.Fatalf("hata tipi ErrExists değil: %T", err)
	}
	if exists.Existing.Value != "10.10.10.10" {
		t.Errorf("hata mevcut değeri göstermiyor: %v", err)
	}
}

// A different type for the same name is a legitimate record, not a duplicate.
func TestAddAllowsDifferentTypeForSameName(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, _ := New("mail.google.com", TypeAAAA, "2001:db8::1", nil)
	if err := f.Add(rec); err != nil {
		t.Fatalf("farklı tip reddedildi: %v", err)
	}
}

func TestDeleteRemovesOnlyTheTargetLine(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	removed := f.Delete("eski.google.com", "", "")
	if removed != 1 {
		t.Fatalf("%d kayıt silindi, beklenen 1", removed)
	}

	out := string(f.Bytes())

	if strings.Contains(out, "eski.google.com") {
		t.Error("kayıt silinmedi")
	}
	// The comment above the deleted record belongs to the operator, not to us.
	if !strings.Contains(out, "# Aşağıdaki kayıt geçicidir") {
		t.Error("silme işlemi yorumu da sildi")
	}
	if !strings.Contains(out, "mail.google.com") || !strings.Contains(out, "www.google.com") {
		t.Error("silme işlemi başka kayıtları etkiledi")
	}
}

func TestDeleteFiltersByType(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, _ := New("mail.google.com", TypeAAAA, "2001:db8::1", nil)
	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}

	if removed := f.Delete("mail.google.com", TypeAAAA, ""); removed != 1 {
		t.Fatalf("%d kayıt silindi, beklenen 1", removed)
	}

	if got := f.Find("mail.google.com", TypeA); len(got) != 1 {
		t.Error("A kaydı yanlışlıkla silindi")
	}
}

func TestUpdateKeepsRecordPosition(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, _ := New("mail.google.com", TypeA, "10.11.12.13", nil)
	if err := f.Update(rec); err != nil {
		t.Fatalf("güncelleme hatası: %v", err)
	}

	out := string(f.Bytes())

	if !strings.Contains(out, `local-data: "mail.google.com. IN A 10.11.12.13"`) {
		t.Errorf("değer güncellenmedi:\n%s", out)
	}
	if strings.Contains(out, "10.10.10.10") {
		t.Error("eski değer kaldı")
	}

	// The updated record must stay ahead of the one that followed it.
	mailIdx := strings.Index(out, "mail.google.com")
	wwwIdx := strings.Index(out, "www.google.com")
	if mailIdx > wwwIdx {
		t.Error("güncelleme kaydın sırasını değiştirdi")
	}
}

func TestUpdateReportsMissingRecord(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	rec, _ := New("yok.google.com", TypeA, "10.0.0.1", nil)

	if err := f.Update(rec); err == nil {
		t.Fatal("olmayan kayıt güncellendi")
	}
}

func TestPruneOnlyRemovesGeneratedZones(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	// Add a record, which generates a zone, then delete the record again.
	rec, _ := New("host.gecici.com", TypeA, "10.70.70.70", nil)
	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}
	f.Delete("host.gecici.com", "", "")

	// Also delete every record of a zone that came from the source file.
	f.Delete("db.internal.local", "", "")
	f.Delete("web.internal.local", "", "")

	f.PruneUnusedZones()

	out := string(f.Bytes())

	if strings.Contains(out, "gecici.com") {
		t.Error("üretilen boş zone temizlenmedi")
	}
	if !strings.Contains(out, `local-zone: "internal.local." static`) {
		t.Error("dosyada var olan zone silindi, ona dokunulmamalıydı")
	}
}

func TestParseHandlesFileWithoutTrailingNewline(t *testing.T) {
	input := `         local-data: "a.example.com. IN A 10.0.0.1"`

	f, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	if got := string(f.Bytes()); got != input {
		t.Errorf("son satır newline'ı uyduruldu:\n%q", got)
	}
}

func TestParseHandlesEmptyFile(t *testing.T) {
	f, err := Parse(nil)
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	if len(f.All()) != 0 {
		t.Error("boş dosyadan kayıt çıktı")
	}
	if got := string(f.Bytes()); got != "" {
		t.Errorf("boş dosya %q oldu", got)
	}
}

func TestParseKeepsUnknownDirectives(t *testing.T) {
	const input = `server:
    verbosity: 1
         local-data: "a.example.com. IN A 10.0.0.1"
    access-control: 10.0.0.0/8 allow
`

	f, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	if got := string(f.Bytes()); got != input {
		t.Errorf("tanınmayan direktifler korunmadı:\n%s", got)
	}
	if len(f.All()) != 1 {
		t.Errorf("%d kayıt bulundu, beklenen 1", len(f.All()))
	}
}

func TestParseAcceptsSingleQuotedValues(t *testing.T) {
	const input = `         local-data: 'note.example.com. IN TXT "tırnaklı metin"'
`

	f, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	all := f.All()
	if len(all) != 1 {
		t.Fatalf("%d kayıt bulundu", len(all))
	}
	if all[0].Type != TypeTXT {
		t.Errorf("tip = %q", all[0].Type)
	}
	if all[0].Value != `"tırnaklı metin"` {
		t.Errorf("değer = %q", all[0].Value)
	}
}

// A value holding a double quote must be written inside single quotes,
// otherwise the generated line would terminate the string early.
func TestGeneratedTXTUsesSingleQuotes(t *testing.T) {
	f, _ := Parse(nil)

	rec, err := New("note.example.com", TypeTXT, `"merhaba"`, nil)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}

	out := string(f.Bytes())

	if !strings.Contains(out, `local-data: 'note.example.com. IN TXT "merhaba"'`) {
		t.Errorf("çift tırnaklı değer tek tırnakla sarılmadı:\n%s", out)
	}
}

func asErrExists(err error, target **ErrExists) bool {
	e, ok := err.(*ErrExists)
	if ok {
		*target = e
	}

	return ok
}
