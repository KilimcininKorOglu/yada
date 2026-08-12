package records

import (
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"mail.google.com":  "mail.google.com.",
		"mail.google.com.": "mail.google.com.",
		"MAIL.Google.COM":  "mail.google.com.",
		"  host.local  ":   "host.local.",
		"":                 "",
	}

	for in, want := range cases {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

func TestParentZone(t *testing.T) {
	cases := map[string]string{
		"mail.google.com":    "google.com.",
		"a.b.c.example.com.": "b.c.example.com.",
		"example.com":        "com.",
		"com.":               "com.",
	}

	for in, want := range cases {
		if got := ParentZone(in); got != want {
			t.Errorf("ParentZone(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

// Declaring a bare TLD transparent would cover every domain under it, which is
// never what adding one record means.
func TestZoneForNeverReturnsBareTLD(t *testing.T) {
	cases := map[string]string{
		"mail.google.com":          "google.com.",
		"a.b.c.example.com.":       "b.c.example.com.",
		"example.com":              "example.com.",
		"test.local":               "test.local.",
		"10.10.10.10.in-addr.arpa": "10.10.10.in-addr.arpa.",
		"host":                     "host.",
	}

	for in, want := range cases {
		if got := ZoneFor(in); got != want {
			t.Errorf("ZoneFor(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

func TestValidateAcceptsValidRecords(t *testing.T) {
	cases := []struct {
		name  string
		typ   Type
		value string
	}{
		{"mail.google.com", TypeA, "10.10.10.10"},
		{"host.example.com", TypeAAAA, "2001:db8::1"},
		{"www.example.com", TypeCNAME, "host.example.com."},
		{"note.example.com", TypeTXT, "kısa bir not"},
		{"example.com", TypeMX, "10 mail.example.com."},
		{"10.10.10.10.in-addr.arpa", TypePTR, "mail.google.com."},
		{"host_1.example.com", TypeA, "10.0.0.1"},
	}

	for _, c := range cases {
		if _, err := New(c.name, c.typ, c.value, nil); err != nil {
			t.Errorf("%s %s %q reddedildi: %v", c.name, c.typ, c.value, err)
		}
	}
}

func TestValidateRejectsWrongValues(t *testing.T) {
	cases := []struct {
		desc  string
		name  string
		typ   Type
		value string
		want  string
	}{
		{"A tipine IPv6", "h.example.com", TypeA, "2001:db8::1", "IPv4"},
		{"A tipine metin", "h.example.com", TypeA, "on nokta bir", "IPv4"},
		{"A tipine eksik oktet", "h.example.com", TypeA, "10.10.10", "IPv4"},
		{"AAAA tipine IPv4", "h.example.com", TypeAAAA, "10.0.0.1", "IPv4 adresi"},
		{"CNAME kendine", "h.example.com", TypeCNAME, "h.example.com", "kendini"},
		{"MX önceliksiz", "example.com", TypeMX, "mail.example.com", "öncelik"},
		{"MX önceliği aralık dışı", "example.com", TypeMX, "70000 mail.example.com", "0-65535"},
		{"PTR düz ad", "h.example.com", TypePTR, "mail.example.com", "in-addr.arpa"},
		{"boş değer", "h.example.com", TypeA, "", "boş"},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			_, err := New(c.name, c.typ, c.value, nil)
			if err == nil {
				t.Fatalf("%s %s %q kabul edildi", c.name, c.typ, c.value)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("hata %q içermiyor: %v", c.want, err)
			}
		})
	}
}

// The PTR error should show the reverse name the user probably wanted.
func TestPTRErrorSuggestsReverseName(t *testing.T) {
	_, err := New("10.10.10.10", TypePTR, "mail.google.com", nil)
	if err == nil {
		t.Fatal("düz IP adı kabul edildi")
	}

	if !strings.Contains(err.Error(), "10.10.10.10.in-addr.arpa.") {
		t.Errorf("hata ters ad önerisi içermiyor: %v", err)
	}
}

func TestReverseName(t *testing.T) {
	got, err := ReverseName("10.10.10.20")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if want := "20.10.10.10.in-addr.arpa."; got != want {
		t.Errorf("IPv4 ters adı = %q, beklenen %q", got, want)
	}

	got, err = ReverseName("2001:db8::1")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if !strings.HasSuffix(got, ".ip6.arpa.") {
		t.Errorf("IPv6 ters adı = %q", got)
	}

	if _, err := ReverseName("bu bir ip degil"); err == nil {
		t.Error("geçersiz IP kabul edildi")
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{
		"example.com",
		"example.com.",
		"a.b.c.d.example.com",
		"host-1.example.com",
		strings.Repeat("a", 63) + ".example.com",
	}

	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("%q reddedildi: %v", name, err)
		}
	}

	invalid := []struct {
		name string
		want string
	}{
		{"", "boş"},
		{".", "yalnızca noktadan"},
		{"a..b.com", "boş etiket"},
		{"-host.example.com", "tire ile"},
		{"host-.example.com", "tire ile"},
		{strings.Repeat("a", 64) + ".example.com", "en fazla 63"},
		{strings.Repeat("a.", 130) + "com", "en fazla 253"},
		{"host name.example.com", "geçersiz karakter"},
		{"host;rm.example.com", "geçersiz karakter"},
	}

	for _, c := range invalid {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateName(c.name)
			if err == nil {
				t.Fatalf("%q kabul edildi", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("hata %q içermiyor: %v", c.want, err)
			}
		})
	}
}

// An internationalised name has to reach Unbound as punycode.
func TestValidateNameAcceptsIDN(t *testing.T) {
	if err := ValidateName("çiçek.example.com"); err != nil {
		t.Errorf("IDN reddedildi: %v", err)
	}

	ascii, err := ToASCII("çiçek.example.com.")
	if err != nil {
		t.Fatalf("punycode çevrimi başarısız: %v", err)
	}
	if !strings.HasPrefix(ascii, "xn--") {
		t.Errorf("punycode üretilmedi: %q", ascii)
	}
	if !strings.HasSuffix(ascii, ".") {
		t.Errorf("sondaki nokta kayboldu: %q", ascii)
	}
}

func TestRecordString(t *testing.T) {
	rec, err := New("mail.google.com", TypeA, "10.10.10.10", nil)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	if got := rec.String(); got != "mail.google.com. IN A 10.10.10.10" {
		t.Errorf("String() = %q", got)
	}

	withTTL, _ := New("mail.google.com", TypeA, "10.10.10.10", SetTTL(3600))
	if got := withTTL.String(); got != "mail.google.com. IN 3600 A 10.10.10.10" &&
		got != "mail.google.com. 3600 IN A 10.10.10.10" {
		t.Errorf("TTL'li String() = %q", got)
	}
}

func TestParseType(t *testing.T) {
	for _, in := range []string{"a", "A", " aaaa ", "CnAmE"} {
		if _, err := ParseType(in); err != nil {
			t.Errorf("ParseType(%q) hata verdi: %v", in, err)
		}
	}

	if _, err := ParseType("SRV"); err == nil {
		t.Error("desteklenmeyen tip kabul edildi")
	}
}

func TestKeyIgnoresValueButFullKeyDoesNot(t *testing.T) {
	a, _ := New("h.example.com", TypeA, "10.0.0.1", nil)
	b, _ := New("h.example.com", TypeA, "10.0.0.2", nil)

	if a.Key() != b.Key() {
		t.Error("Key değere göre değişti, çakışma tespiti bozulur")
	}
	if a.FullKey() == b.FullKey() {
		t.Error("FullKey değeri yok sayıyor, fark tespiti bozulur")
	}
}
