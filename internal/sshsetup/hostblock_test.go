package sshsetup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handWritten is a configuration the user maintains themselves, which every
// edit has to leave intact.
const handWritten = `Host bastion.example.com
    User operator
    ForwardAgent yes

Host *.internal
    ProxyJump bastion.example.com
`

func configPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "config")
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya okunamadı: %v", err)
	}

	return string(data)
}

func entry(pattern string) HostEntry {
	return HostEntry{
		Pattern:      pattern,
		HostName:     pattern,
		User:         "user01",
		Port:         2222,
		IdentityFile: "/home/kullanici/.ssh/yada_ns1",
	}
}

func TestUpsertHostBlockCreatesTheFile(t *testing.T) {
	path := configPath(t)

	if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	text := readFile(t, path)

	for _, want := range []string{
		"Host 192.0.2.4",
		"HostName 192.0.2.4",
		"User user01",
		"Port 2222",
		"IdentityFile /home/kullanici/.ssh/yada_ns1",
		"IdentitiesOnly yes",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%q yazılmadı:\n%s", want, text)
		}
	}
}

// A second call for the same server replaces the block instead of adding
// another one, or ssh would keep applying the first.
func TestUpsertHostBlockReplacesItsOwnBlock(t *testing.T) {
	path := configPath(t)

	if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
		t.Fatalf("ilk yazma hatası: %v", err)
	}

	updated := entry("192.0.2.4")
	updated.User = "başkası"
	updated.Port = 22

	if err := UpsertHostBlock(path, updated); err != nil {
		t.Fatalf("ikinci yazma hatası: %v", err)
	}

	text := readFile(t, path)

	if got := strings.Count(text, "Host 192.0.2.4"); got != 1 {
		t.Errorf("blok %d kez var, 1 olmalı:\n%s", got, text)
	}
	if !strings.Contains(text, "User başkası") {
		t.Errorf("blok güncellenmedi:\n%s", text)
	}
	if strings.Contains(text, "User user01") {
		t.Errorf("eski değer kaldı:\n%s", text)
	}
}

// Everything outside the markers belongs to the user.
func TestUpsertHostBlockKeepsForeignContent(t *testing.T) {
	path := configPath(t)

	if err := os.WriteFile(path, []byte(handWritten), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	text := readFile(t, path)

	for _, want := range []string{
		"Host bastion.example.com",
		"ForwardAgent yes",
		"Host *.internal",
		"ProxyJump bastion.example.com",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("kullanıcının satırı kayboldu (%q):\n%s", want, text)
		}
	}
}

// A block the user wrote for the same address may carry settings this tool
// knows nothing about, so it is reported rather than overwritten.
func TestUpsertHostBlockRefusesAForeignBlockForTheSameHost(t *testing.T) {
	path := configPath(t)

	original := "Host 192.0.2.4\n    User elle\n    ProxyJump bastion\n"

	if err := os.WriteFile(path, []byte(original), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	err := UpsertHostBlock(path, entry("192.0.2.4"))

	if !errors.Is(err, ErrForeignHostBlock) {
		t.Fatalf("hata = %v, ErrForeignHostBlock olmalıydı", err)
	}

	if got := readFile(t, path); got != original {
		t.Errorf("reddedilen yazma dosyayı değiştirdi:\n%s", got)
	}
}

func TestUpsertHostBlockBacksUpBeforeWriting(t *testing.T) {
	path := configPath(t)

	if err := os.WriteFile(path, []byte(handWritten), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if got := readFile(t, path+BackupSuffix); got != handWritten {
		t.Errorf("yedek özgün içeriği tutmuyor:\n%s", got)
	}
}

// Repeated edits must not pile up blank lines.
func TestUpsertHostBlockKeepsSpacingStable(t *testing.T) {
	path := configPath(t)

	if err := os.WriteFile(path, []byte(handWritten), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	for range 3 {
		if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
			t.Fatalf("beklenmeyen hata: %v", err)
		}
	}

	if text := readFile(t, path); strings.Contains(text, "\n\n\n") {
		t.Errorf("tekrarlanan yazma boş satır biriktirdi:\n%s", text)
	}
}

func TestRemoveHostBlockLeavesForeignBlocksAlone(t *testing.T) {
	path := configPath(t)

	if err := os.WriteFile(path, []byte(handWritten), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
		t.Fatalf("yazma hatası: %v", err)
	}

	if err := RemoveHostBlock(path, "192.0.2.4"); err != nil {
		t.Fatalf("silme hatası: %v", err)
	}

	text := readFile(t, path)

	if strings.Contains(text, "192.0.2.4") {
		t.Errorf("blok silinmedi:\n%s", text)
	}
	if !strings.Contains(text, "Host bastion.example.com") {
		t.Errorf("kullanıcının bloğu da silindi:\n%s", text)
	}
}

// The pattern must match a whole Host line, not a mention inside another
// value, or an unrelated ProxyJump would block the write.
func TestUpsertHostBlockIgnoresAMentionInsideAnotherDirective(t *testing.T) {
	path := configPath(t)

	original := "Host tunnel\n    ProxyJump 192.0.2.4\n"

	if err := os.WriteFile(path, []byte(original), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	if err := UpsertHostBlock(path, entry("192.0.2.4")); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if text := readFile(t, path); !strings.Contains(text, "Host 192.0.2.4") {
		t.Errorf("blok yazılmadı:\n%s", text)
	}
}
