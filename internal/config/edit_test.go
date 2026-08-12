package config

import (
	"strings"
	"testing"
)

// commentedFile carries a comment in every position the encoder has to
// preserve: above a section, above a nested key, and at the end of a line.
const commentedFile = `# Üst başlık.

# Sunucular.
servers:
  - name: ns1
    host: 192.0.2.4

# Varsayılanlar.
defaults:
  user: user01
  # Kayıt dosyası.
  records_file: /etc/unbound/local_records.conf
  main_config: /etc/unbound/unbound.conf
`

func TestAddServerAppendsToTheList(t *testing.T) {
	out, err := AddServer([]byte(commentedFile), Server{Name: "ns2", Host: "192.0.2.5"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	cfg, err := Decode(out)
	if err != nil {
		t.Fatalf("sonuç yüklenemedi: %v\n%s", err, out)
	}

	if len(cfg.Servers) != 2 {
		t.Fatalf("%d sunucu var, 2 olmalı:\n%s", len(cfg.Servers), out)
	}

	// The new one goes last, so the file reads in the order servers were added.
	if cfg.Servers[1].Host != "192.0.2.5" {
		t.Errorf("yeni sunucu sona eklenmedi: %+v", cfg.Servers)
	}
}

// The comments are the documentation of every key, so an edit that drops them
// makes the file worse each time it is used.
func TestAddServerKeepsEveryComment(t *testing.T) {
	out, err := AddServer([]byte(commentedFile), Server{Host: "192.0.2.5", User: "user01"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	text := string(out)

	for _, comment := range []string{
		"# Üst başlık.",
		"# Sunucular.",
		"# Varsayılanlar.",
		"# Kayıt dosyası.",
	} {
		if !strings.Contains(text, comment) {
			t.Errorf("%q yorumu kayboldu:\n%s", comment, text)
		}
	}
}

// A field left empty has to stay out of the file, so the server keeps
// inheriting it from defaults instead of being pinned to today's value.
func TestAddServerOmitsEmptyFields(t *testing.T) {
	out, err := AddServer([]byte(commentedFile), Server{Host: "192.0.2.5", User: "user01"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	text := string(out)

	for _, unwanted := range []string{"records_file: /etc/unbound/local_records.conf\n    ", "port:"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("boş alan yazıldı (%q):\n%s", unwanted, text)
		}
	}

	// The value still resolves, through defaults.
	cfg, err := Decode(out)
	if err != nil {
		t.Fatalf("sonuç yüklenemedi: %v", err)
	}

	if got := cfg.Servers[1].RecordsFile; got != "/etc/unbound/local_records.conf" {
		t.Errorf("records_file defaults'tan gelmedi: %q", got)
	}
}

func TestAddServerWritesTheFieldsItWasGiven(t *testing.T) {
	sudo := false

	out, err := AddServer([]byte(commentedFile), Server{
		Name:        "ns2",
		Host:        "192.0.2.5",
		User:        "başka",
		Port:        2222,
		RecordsFile: "/etc/unbound/other.conf",
		MainConfig:  "/etc/unbound/main.conf",
		Sudo:        &sudo,
	})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	text := string(out)

	for _, want := range []string{
		"name: ns2",
		"host: 192.0.2.5",
		"port: 2222",
		"records_file: /etc/unbound/other.conf",
		"main_config: /etc/unbound/main.conf",
		"sudo: false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%q yazılmadı:\n%s", want, text)
		}
	}
}

func TestAddServerRejectsAFileWithoutServers(t *testing.T) {
	_, err := AddServer([]byte("defaults:\n  user: user01\n"), Server{Host: "192.0.2.5"})

	if err == nil {
		t.Fatal("servers listesi olmayan dosya kabul edildi")
	}
}

func TestAddServerRejectsAServersKeyThatIsNotAList(t *testing.T) {
	_, err := AddServer([]byte("servers: hayır\n"), Server{Host: "192.0.2.5"})

	if err == nil {
		t.Fatal("liste olmayan servers kabul edildi")
	}
}

func TestClearServersEmptiesTheListAndKeepsTheRest(t *testing.T) {
	out, err := ClearServers([]byte(commentedFile))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	text := string(out)

	if strings.Contains(text, "192.0.2.4") {
		t.Errorf("örnek sunucu silinmedi:\n%s", text)
	}
	if !strings.Contains(text, "# Varsayılanlar.") || !strings.Contains(text, "user: user01") {
		t.Errorf("diğer bölümler korunmadı:\n%s", text)
	}
}

// The shipped example is the starting point on a machine with no
// configuration, so emptying it and adding one server has to produce a file the
// application can load.
func TestClearThenAddProducesALoadableFile(t *testing.T) {
	empty, err := ClearServers(Example)
	if err != nil {
		t.Fatalf("örnek boşaltılamadı: %v", err)
	}

	out, err := AddServer(empty, Server{Name: "ns1", Host: "192.0.2.4", User: "user01"})
	if err != nil {
		t.Fatalf("sunucu eklenemedi: %v", err)
	}

	cfg, err := Decode(out)
	if err != nil {
		t.Fatalf("sonuç yüklenemedi: %v\n%s", err, out)
	}

	if len(cfg.Servers) != 1 || cfg.Servers[0].Host != "192.0.2.4" {
		t.Errorf("beklenen tek sunucu yok: %+v", cfg.Servers)
	}

	// The emptied list must come back as a block sequence, not the flow style
	// an empty list needs.
	if !strings.Contains(string(out), "servers:\n  - name: ns1") {
		t.Errorf("liste blok biçiminde yazılmadı:\n%s", out)
	}
}

// Sections stay separated by a blank line, and running the edit again must not
// add more of them.
func TestSectionSpacingIsRestoredAndStable(t *testing.T) {
	once, err := AddServer([]byte(commentedFile), Server{Host: "192.0.2.5", User: "user01"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if !strings.Contains(string(once), "\n\n# Varsayılanlar.") {
		t.Errorf("bölüm öncesi boş satır konmadı:\n%s", once)
	}

	twice, err := AddServer(once, Server{Host: "192.0.2.6", User: "user01"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if strings.Contains(string(twice), "\n\n\n") {
		t.Errorf("ikinci düzenleme fazladan boş satır ekledi:\n%s", twice)
	}
}
