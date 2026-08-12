package sshsetup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// generateKey produces a real key with the tool that will later validate it,
// so the fixture cannot drift from what OpenSSH actually accepts. No key is
// kept in the repository.
func generateKey(t *testing.T) []byte {
	t.Helper()

	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen kurulu değil")
	}

	path := filepath.Join(t.TempDir(), "id_test")

	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "test", "-f", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("anahtar üretilemedi: %v\n%s", err, out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("anahtar okunamadı: %v", err)
	}

	return data
}

func TestWriteKeyStoresTheKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".ssh")
	key := generateKey(t)

	res, err := WriteKey(t.Context(), dir, "ns1", key)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if res.Reused {
		t.Error("ilk yazma yeniden kullanım olarak raporlandı")
	}

	if filepath.Base(res.Path) != "yada_ns1" {
		t.Errorf("dosya adı = %q", filepath.Base(res.Path))
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("yazılan anahtar okunamadı: %v", err)
	}

	if string(data) != string(normaliseKey(key)) {
		t.Error("yazılan içerik verilen anahtarla aynı değil")
	}
}

// OpenSSH refuses a key whose file or directory is readable by anyone else.
func TestWriteKeyLeavesThePermissionsSSHRequires(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".ssh")

	res, err := WriteKey(t.Context(), dir, "ns1", generateKey(t))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	keyInfo, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("anahtar bulunamadı: %v", err)
	}

	if mode := keyInfo.Mode().Perm(); mode != KeyMode {
		t.Errorf("anahtar izni = %o, %o olmalı", mode, KeyMode)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("dizin bulunamadı: %v", err)
	}

	if mode := dirInfo.Mode().Perm(); mode != DirMode {
		t.Errorf("dizin izni = %o, %o olmalı", mode, DirMode)
	}
}

// Servers may share a key, and copying it per server would leave several files
// to rotate instead of one.
func TestWriteKeyReusesAnIdenticalKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".ssh")
	key := generateKey(t)

	first, err := WriteKey(t.Context(), dir, "ns1", key)
	if err != nil {
		t.Fatalf("ilk yazma hatası: %v", err)
	}

	second, err := WriteKey(t.Context(), dir, "ns2", key)
	if err != nil {
		t.Fatalf("ikinci yazma hatası: %v", err)
	}

	if !second.Reused {
		t.Error("aynı anahtar yeniden kullanılmadı")
	}
	if second.Path != first.Path {
		t.Errorf("ikinci yol = %q, ilkiyle aynı olmalıydı (%q)", second.Path, first.Path)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("dizin okunamadı: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("dizinde %d dosya var, 1 olmalı", len(entries))
	}
}

func TestWriteKeyWritesASecondFileForADifferentKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".ssh")

	if _, err := WriteKey(t.Context(), dir, "ns1", generateKey(t)); err != nil {
		t.Fatalf("ilk yazma hatası: %v", err)
	}

	res, err := WriteKey(t.Context(), dir, "ns2", generateKey(t))
	if err != nil {
		t.Fatalf("ikinci yazma hatası: %v", err)
	}

	if res.Reused {
		t.Error("farklı anahtar yeniden kullanım sayıldı")
	}
	if filepath.Base(res.Path) != "yada_ns2" {
		t.Errorf("dosya adı = %q", filepath.Base(res.Path))
	}
}

// A truncated paste has to be caught here, because ssh reports it much later
// with a message that says nothing about where the file came from.
func TestWriteKeyRejectsAnUnreadableKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen kurulu değil")
	}

	dir := filepath.Join(t.TempDir(), ".ssh")

	broken := "-----BEGIN OPENSSH PRIVATE KEY-----\nbozuk\n-----END OPENSSH PRIVATE KEY-----"

	if _, err := WriteKey(t.Context(), dir, "ns1", []byte(broken)); err == nil {
		t.Fatal("bozuk anahtar kabul edildi")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("dizin okunamadı: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("reddedilen anahtardan dosya kaldı: %v", entries)
	}
}

// The message must not carry the key or the tool's echo of it, because it
// reaches the log panel and from there the log file.
func TestWriteKeyErrorDoesNotLeakTheKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen kurulu değil")
	}

	dir := filepath.Join(t.TempDir(), ".ssh")

	secret := "SIZINTI-OLMAMALI"
	broken := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + secret + "\n-----END OPENSSH PRIVATE KEY-----"

	_, err := WriteKey(t.Context(), dir, "ns1", []byte(broken))
	if err == nil {
		t.Fatal("bozuk anahtar kabul edildi")
	}

	if strings.Contains(err.Error(), secret) {
		t.Errorf("hata mesajı anahtar içeriğini taşıyor: %v", err)
	}
}

func TestWriteKeyRejectsAnEmptyKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".ssh")

	if _, err := WriteKey(context.Background(), dir, "ns1", []byte("   \n\t ")); err == nil {
		t.Fatal("boş anahtar kabul edildi")
	}
}

// A pasted key arrives with whatever line endings the clipboard had, and
// OpenSSH accepts neither CRLF nor a missing final newline.
func TestNormaliseKey(t *testing.T) {
	got := string(normaliseKey([]byte("  satır1\r\nsatır2\r\n  ")))

	if got != "satır1\nsatır2\n" {
		t.Errorf("normalleştirme = %q", got)
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"ns1":              "ns1",
		"ns 1":             "ns_1",
		"../../etc/shadow": "etc_shadow",
		"":                 "key",
		"...":              "key",
	}

	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, beklenen %q", in, got, want)
		}
	}
}
