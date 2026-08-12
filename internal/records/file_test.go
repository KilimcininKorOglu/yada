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

// A transparent zone with no records does nothing, so it is safe to drop. Any
// other zone type is a deliberate policy and must survive.
func TestPruneRemovesEmptyTransparentZonesOnly(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	// Empty out both zones: google.com. is transparent, internal.local. is
	// static.
	f.Delete("mail.google.com", "", "")
	f.Delete("www.google.com", "", "")
	f.Delete("eski.google.com", "", "")
	f.Delete("db.internal.local", "", "")
	f.Delete("web.internal.local", "", "")

	if removed := f.PruneUnusedZones(); removed != 1 {
		t.Errorf("%d zone silindi, yalnızca transparent olan silinmeliydi", removed)
	}

	out := string(f.Bytes())

	if strings.Contains(out, `local-zone: "google.com."`) {
		t.Error("boş transparent zone temizlenmedi")
	}
	if !strings.Contains(out, `local-zone: "internal.local." static`) {
		t.Error("static zone silindi, kayıtsız da olsa korunmalıydı")
	}
}

// Pruning must not touch a zone that still has records, even indirectly.
func TestPruneKeepsZonesInUse(t *testing.T) {
	f, _ := Parse([]byte(existingFile))

	f.Delete("www.google.com", "", "")

	if removed := f.PruneUnusedZones(); removed != 0 {
		t.Errorf("%d zone silindi, hâlâ kayıt varken silinmemeliydi", removed)
	}

	if !strings.Contains(string(f.Bytes()), `local-zone: "google.com."`) {
		t.Error("kullanımdaki zone silindi")
	}
}

// The tool cannot tell its own zone lines from the operator's once the file
// has been read back, so a freshly parsed empty transparent zone is pruned
// just the same.
func TestPruneWorksOnZonesReadFromFile(t *testing.T) {
	const input = `         local-zone: "bos.com." transparent
         local-zone: "dolu.com." transparent
         local-data: "host.dolu.com. IN A 10.0.0.1"
`

	f, _ := Parse([]byte(input))

	if removed := f.PruneUnusedZones(); removed != 1 {
		t.Fatalf("%d zone silindi, beklenen 1", removed)
	}

	out := string(f.Bytes())

	if strings.Contains(out, "bos.com") {
		t.Error("dosyadan okunan boş transparent zone temizlenmedi")
	}
	if !strings.Contains(out, "dolu.com") {
		t.Error("kullanımdaki zone silindi")
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

// TXT data is a character-string: unquoted, a value with spaces would be read
// as several strings instead of one.
func TestGeneratedTXTIsQuoted(t *testing.T) {
	f, _ := Parse(nil)

	rec, err := New("note.example.com", TypeTXT, "v=spf1 include:_spf.google.com ~all", nil)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}

	out := string(f.Bytes())

	// The inner quotes force the outer delimiter to become a single quote.
	want := `local-data: 'note.example.com. IN TXT "v=spf1 include:_spf.google.com ~all"'`
	if !strings.Contains(out, want) {
		t.Errorf("TXT değeri tırnaklanmadı:\ngelen:\n%s\nbeklenen satır:\n%s", out, want)
	}
}

// A value the user already quoted must not be quoted twice.
func TestGeneratedTXTDoesNotDoubleQuote(t *testing.T) {
	f, _ := Parse(nil)

	rec, _ := New("note.example.com", TypeTXT, `"zaten tırnaklı"`, nil)
	if err := f.Add(rec); err != nil {
		t.Fatalf("ekleme hatası: %v", err)
	}

	out := string(f.Bytes())

	if strings.Contains(out, `""`) {
		t.Errorf("değer iki kez tırnaklandı:\n%s", out)
	}
}

// A round trip through the parser must not change a quoted TXT record.
func TestTXTRoundTrip(t *testing.T) {
	const input = `         local-data: 'note.example.com. IN TXT "v=spf1 ~all"'
`

	f, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	if got := string(f.Bytes()); got != input {
		t.Errorf("TXT round-trip bozuldu:\n%s", got)
	}
}

func asErrExists(err error, target **ErrExists) bool {
	e, ok := err.(*ErrExists)
	if ok {
		*target = e
	}

	return ok
}
