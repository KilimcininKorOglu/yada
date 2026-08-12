package bulk

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kerem/unbound-dns/internal/records"
)

func TestImportReadsRows(t *testing.T) {
	const input = `name,type,value,ttl
mail.google.com,A,10.10.10.10,
www.google.com,A,10.30.30.30,3600
ipv6.example.com,AAAA,2001:db8::1,
`

	res, err := Import(strings.NewReader(input))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if len(res.Errors) != 0 {
		t.Fatalf("hatalı satır bildirildi: %v", res.Errors)
	}
	if len(res.Records) != 3 {
		t.Fatalf("%d kayıt okundu, beklenen 3", len(res.Records))
	}

	if res.Records[0].Name != "mail.google.com." {
		t.Errorf("ad = %q", res.Records[0].Name)
	}
	if res.Records[0].TTL != nil {
		t.Error("boş ttl sütunu için TTL atandı")
	}
	if res.Records[1].TTL == nil || *res.Records[1].TTL != 3600 {
		t.Errorf("ttl okunmadı: %v", res.Records[1].TTL)
	}
}

// Column order is free, since columns are matched by header name.
func TestImportAcceptsAnyColumnOrder(t *testing.T) {
	const input = `ttl,value,name,type
300,10.0.0.1,host.example.com,A
`

	res, err := Import(strings.NewReader(input))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if len(res.Records) != 1 {
		t.Fatalf("%d kayıt okundu", len(res.Records))
	}
	if res.Records[0].Value != "10.0.0.1" {
		t.Errorf("değer = %q", res.Records[0].Value)
	}
	if res.Records[0].TTL == nil || *res.Records[0].TTL != 300 {
		t.Error("ttl okunmadı")
	}
}

// One bad row must not cost the whole file: the valid rows still import and
// the failure is reported with its line number.
func TestImportReportsBadRowsAndKeepsGoodOnes(t *testing.T) {
	const input = `name,type,value,ttl
mail.google.com,A,10.10.10.10,
bozuk.example.com,A,bu bir ip degil,
alias.example.com,SRV,host.example.com.,
www.google.com,A,10.30.30.30,
eksik.example.com,A,,
ttlbozuk.example.com,A,10.0.0.9,cok
`

	res, err := Import(strings.NewReader(input))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if len(res.Records) != 2 {
		t.Errorf("%d geçerli kayıt okundu, beklenen 2", len(res.Records))
	}
	if len(res.Errors) != 4 {
		t.Fatalf("%d hatalı satır bildirildi, beklenen 4: %v", len(res.Errors), res.Errors)
	}

	// Line numbers must point at the real line, header included.
	wantLines := []int{3, 4, 6, 7}
	for i, want := range wantLines {
		if res.Errors[i].Line != want {
			t.Errorf("hata %d satır %d gösterdi, beklenen %d", i, res.Errors[i].Line, want)
		}
	}

	if !strings.Contains(res.Errors[0].Error(), "IPv4") {
		t.Errorf("değer hatası açıklanmadı: %v", res.Errors[0])
	}
	if !strings.Contains(res.Errors[1].Error(), "SRV") {
		t.Errorf("tip hatası açıklanmadı: %v", res.Errors[1])
	}
	if !strings.Contains(res.Errors[3].Error(), "ttl") {
		t.Errorf("ttl hatası açıklanmadı: %v", res.Errors[3])
	}
}

func TestImportRejectsMissingColumns(t *testing.T) {
	const input = `name,value
mail.google.com,10.10.10.10
`

	_, err := Import(strings.NewReader(input))
	if err == nil {
		t.Fatal("eksik sütunlu başlık kabul edildi")
	}
	if !strings.Contains(err.Error(), "type") {
		t.Errorf("eksik sütun adı bildirilmedi: %v", err)
	}
}

func TestImportSkipsBlankLines(t *testing.T) {
	const input = `name,type,value,ttl
mail.google.com,A,10.10.10.10,

www.google.com,A,10.30.30.30,
`

	res, err := Import(strings.NewReader(input))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if len(res.Errors) != 0 {
		t.Errorf("boş satır hata sayıldı: %v", res.Errors)
	}
	if len(res.Records) != 2 {
		t.Errorf("%d kayıt okundu", len(res.Records))
	}
}

func TestImportRejectsEmptyFile(t *testing.T) {
	if _, err := Import(strings.NewReader("")); err == nil {
		t.Fatal("boş dosya kabul edildi")
	}
}

func TestExportProducesImportableOutput(t *testing.T) {
	original := []records.Record{
		mustRecord(t, "mail.google.com", records.TypeA, "10.10.10.10", nil),
		mustRecord(t, "www.google.com", records.TypeA, "10.30.30.30", new(uint32(3600))),
		mustRecord(t, "note.example.com", records.TypeTXT, "bir not", nil),
		mustRecord(t, "example.com", records.TypeMX, "10 mail.example.com.", nil),
	}

	var buf bytes.Buffer
	if err := Export(&buf, original); err != nil {
		t.Fatalf("dışa aktarma hatası: %v", err)
	}

	res, err := Import(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("dışa aktarılan dosya içe aktarılamadı: %v", err)
	}

	if len(res.Errors) != 0 {
		t.Fatalf("round-trip hata verdi: %v", res.Errors)
	}
	if len(res.Records) != len(original) {
		t.Fatalf("%d kayıt geri okundu, beklenen %d", len(res.Records), len(original))
	}

	for i, want := range original {
		got := res.Records[i]

		if got.Name != want.Name || got.Type != want.Type || got.Value != want.Value {
			t.Errorf("kayıt %d bozuldu:\ngelen  = %s\nbeklenen = %s", i, got.String(), want.String())
		}

		switch {
		case want.TTL == nil && got.TTL != nil:
			t.Errorf("kayıt %d: TTL uyduruldu", i)
		case want.TTL != nil && got.TTL == nil:
			t.Errorf("kayıt %d: TTL kayboldu", i)
		case want.TTL != nil && *got.TTL != *want.TTL:
			t.Errorf("kayıt %d: TTL = %d, beklenen %d", i, *got.TTL, *want.TTL)
		}
	}
}

// A value containing a comma must survive the round trip.
func TestExportQuotesValuesWithCommas(t *testing.T) {
	recs := []records.Record{
		mustRecord(t, "note.example.com", records.TypeTXT, "bir, iki, üç", nil),
	}

	var buf bytes.Buffer
	if err := Export(&buf, recs); err != nil {
		t.Fatalf("dışa aktarma hatası: %v", err)
	}

	res, err := Import(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("içe aktarma hatası: %v", err)
	}

	if len(res.Records) != 1 {
		t.Fatalf("%d kayıt okundu: %v", len(res.Records), res.Errors)
	}
	if res.Records[0].Value != "bir, iki, üç" {
		t.Errorf("virgüllü değer bozuldu: %q", res.Records[0].Value)
	}
}

func mustRecord(t *testing.T, name string, typ records.Type, value string, ttl *uint32) records.Record {
	t.Helper()

	rec, err := records.New(name, typ, value, ttl)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	return rec
}
