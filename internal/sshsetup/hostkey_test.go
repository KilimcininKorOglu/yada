package sshsetup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Two distinct key materials for the same host, which is the situation
// CheckKnownHosts exists to catch.
const (
	keyMaterialA = "AAAAC3NzaC1lZDI1NTE5AAAAIGaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	keyMaterialB = "AAAAC3NzaC1lZDI1NTE5AAAAIGbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func scanFor(host string, port int, material string) Scan {
	line := hostPattern(host, port) + " ssh-ed25519 " + material

	return Scan{
		Host: host,
		Port: port,
		Keys: []HostKey{{
			Line:        line,
			Type:        "ED25519",
			Fingerprint: "SHA256:sahte",
		}},
	}
}

func knownHostsWith(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "known_hosts")

	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}

	if err := os.WriteFile(path, []byte(body), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	return path
}

func TestCheckKnownHostsReportsAnUnknownHost(t *testing.T) {
	path := knownHostsWith(t, "başka.example.com ssh-ed25519 "+keyMaterialA)

	state, err := CheckKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialB))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostUnknown {
		t.Errorf("durum = %v, HostUnknown olmalıydı", state)
	}
}

func TestCheckKnownHostsReportsAMatch(t *testing.T) {
	path := knownHostsWith(t, "192.0.2.4 ssh-ed25519 "+keyMaterialA)

	state, err := CheckKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialA))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostMatches {
		t.Errorf("durum = %v, HostMatches olmalıydı", state)
	}
}

// A different key for a host already in the file is either a rebuilt server or
// something in the way, and neither may be written over silently.
func TestCheckKnownHostsReportsAChangedKey(t *testing.T) {
	path := knownHostsWith(t, "192.0.2.4 ssh-ed25519 "+keyMaterialA)

	state, err := CheckKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialB))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostChanged {
		t.Errorf("durum = %v, HostChanged olmalıydı", state)
	}
}

// known_hosts brackets a host that is not on port 22, so the plain name would
// not find the entry.
func TestCheckKnownHostsMatchesABracketedPort(t *testing.T) {
	path := knownHostsWith(t, "[127.0.0.1]:8340 ssh-ed25519 "+keyMaterialA)

	state, err := CheckKnownHosts(path, scanFor("127.0.0.1", 8340, keyMaterialA))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostMatches {
		t.Errorf("durum = %v, HostMatches olmalıydı", state)
	}
}

// The same address on another port is another machine whenever forwarding is
// involved, so its entry must not be read as this one's.
func TestCheckKnownHostsKeepsPortsApart(t *testing.T) {
	path := knownHostsWith(t, "[127.0.0.1]:8340 ssh-ed25519 "+keyMaterialA)

	state, err := CheckKnownHosts(path, scanFor("127.0.0.1", 8342, keyMaterialB))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostUnknown {
		t.Errorf("durum = %v, HostUnknown olmalıydı", state)
	}
}

func TestCheckKnownHostsMatchesAHostListedWithOthers(t *testing.T) {
	path := knownHostsWith(t, "ns1.example.com,192.0.2.4 ssh-ed25519 "+keyMaterialA)

	state, err := CheckKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialA))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostMatches {
		t.Errorf("durum = %v, HostMatches olmalıydı", state)
	}
}

func TestCheckKnownHostsHandlesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")

	state, err := CheckKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialA))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostUnknown {
		t.Errorf("durum = %v, HostUnknown olmalıydı", state)
	}
}

func TestAppendKnownHostsCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gizli", "known_hosts")

	if err := AppendKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialA)); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya okunamadı: %v", err)
	}

	if !strings.Contains(string(data), keyMaterialA) {
		t.Errorf("anahtar eklenmedi:\n%s", data)
	}
}

// A file whose last line has no newline would otherwise be joined to the new
// entry, leaving both unusable.
func TestAppendKnownHostsSeparatesFromAnUnterminatedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")

	if err := os.WriteFile(path, []byte("eski.example.com ssh-ed25519 "+keyMaterialB), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	if err := AppendKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialA)); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya okunamadı: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("%d satır var, 2 olmalı:\n%s", len(lines), data)
	}
}

func TestAppendKnownHostsBacksUpFirst(t *testing.T) {
	original := "eski.example.com ssh-ed25519 " + keyMaterialB + "\n"
	path := knownHostsWith(t, strings.TrimRight(original, "\n"))

	if err := AppendKnownHosts(path, scanFor("192.0.2.4", 0, keyMaterialA)); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	data, err := os.ReadFile(path + BackupSuffix)
	if err != nil {
		t.Fatalf("yedek okunamadı: %v", err)
	}

	if string(data) != original {
		t.Errorf("yedek özgün içeriği tutmuyor:\n%s", data)
	}
}

// describeKey parses what ssh-keygen prints, so the format is pinned against
// the real tool rather than against a copied string.
func TestDescribeKeyReadsTheFingerprintAndType(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen kurulu değil")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "id_test")

	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "test", "-f", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("anahtar üretilemedi: %v\n%s", err, out)
	}

	pub, err := os.ReadFile(path + ".pub")
	if err != nil {
		t.Fatalf("public key okunamadı: %v", err)
	}

	// A known_hosts line is the host pattern followed by the public key.
	line := "[127.0.0.1]:8340 " + strings.TrimSpace(string(pub))

	key, err := describeKey(t.Context(), line)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Errorf("parmak izi = %q, SHA256: ile başlamalıydı", key.Fingerprint)
	}
	if key.Type != "ED25519" {
		t.Errorf("tip = %q, ED25519 olmalıydı", key.Type)
	}
	if key.Line != line {
		t.Errorf("known_hosts satırı değiştirildi: %q", key.Line)
	}
}

func TestScanFingerprintsListsEveryKey(t *testing.T) {
	scan := Scan{Keys: []HostKey{
		{Type: "ED25519", Fingerprint: "SHA256:bir"},
		{Type: "RSA", Fingerprint: "SHA256:iki"},
	}}

	want := "ED25519 SHA256:bir\nRSA SHA256:iki"
	if got := scan.Fingerprints(); got != want {
		t.Errorf("parmak izleri = %q", got)
	}
}
