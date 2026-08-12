package main

import (
	"slices"
	"strings"
	"testing"
)

func TestSelectMode(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantGUI bool
		wantRes []string
	}{
		{
			name:    "argümansız çalıştırma arayüzü açar",
			args:    nil,
			wantGUI: true,
		},
		{
			name:    "-cli komut satırına geçer",
			args:    []string{"-cli", "list"},
			wantRes: []string{"list"},
		},
		{
			name:    "--cli de kabul edilir",
			args:    []string{"--cli"},
			wantRes: []string{},
		},
		{
			name:    "alt komut verildiyse komut satırı çalışır",
			args:    []string{"list", "--type", "A"},
			wantRes: []string{"list", "--type", "A"},
		},
		{
			name:    "--help arayüz açmaz",
			args:    []string{"--help"},
			wantRes: []string{"--help"},
		},
		{
			name:    "--gui arayüzü zorlar",
			args:    []string{"--gui", "--config", "/tmp/a.conf"},
			wantGUI: true,
			wantRes: []string{"--config", "/tmp/a.conf"},
		},
		{
			// A record value that happens to look like the flag must not
			// change which interface runs.
			name:    "ilk argüman dışındaki -cli bir değerdir",
			args:    []string{"add", "a.example.com", "TXT", "-cli"},
			wantRes: []string{"add", "a.example.com", "TXT", "-cli"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotGUI, gotRest := selectMode(tc.args)

			if gotGUI != tc.wantGUI {
				t.Errorf("arayüz = %v, %v olmalıydı", gotGUI, tc.wantGUI)
			}
			// slices.Equal compares lengths first, so a nil and an empty
			// slice count as the same result.
			if !slices.Equal(gotRest, tc.wantRes) {
				t.Errorf("kalan argümanlar = %v, %v olmalıydı", gotRest, tc.wantRes)
			}
		})
	}
}

func TestGUIArgs(t *testing.T) {
	path, err := guiArgs([]string{"--config", "/etc/unbound-dns.conf"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if path != "/etc/unbound-dns.conf" {
		t.Errorf("ayar yolu = %q", path)
	}

	path, err = guiArgs([]string{"--config=/etc/unbound-dns.conf"})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if path != "/etc/unbound-dns.conf" {
		t.Errorf("eşittirli biçim okunamadı, ayar yolu = %q", path)
	}

	if _, err := guiArgs(nil); err != nil {
		t.Errorf("argümansız çağrı hata verdi: %v", err)
	}
}

func TestGUIArgsRejectsUnknownFlag(t *testing.T) {
	_, err := guiArgs([]string{"--json"})
	if err == nil {
		t.Fatal("tanınmayan seçenek kabul edildi")
	}

	// The message has to point at the command line, since that is where the
	// flag the user typed actually works.
	if !strings.Contains(err.Error(), "-cli") {
		t.Errorf("hata mesajı komut satırına yönlendirmiyor: %v", err)
	}
}

func TestGUIArgsRejectsConfigWithoutValue(t *testing.T) {
	if _, err := guiArgs([]string{"--config"}); err == nil {
		t.Fatal("değersiz --config kabul edildi")
	}
}
