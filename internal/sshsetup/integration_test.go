package sshsetup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run against the container stack from docker-compose.yml, which
// is the only way to prove that the files this package writes actually
// authenticate. Everything else here checks the text; this checks the result.
//
// They skip when the stack is down, so an ordinary run and CI are unaffected:
//
//	make docker-up && go test -count=1 ./internal/sshsetup/
const (
	testHost = "127.0.0.1"
	testPort = 8340
	testUser = "user01"
)

// requireStack skips unless the container is answering on the ssh port.
func requireStack(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("ssh-keyscan"); err != nil {
		t.Skip("ssh-keyscan kurulu değil")
	}

	if _, err := ScanHost(t.Context(), testHost, testPort); err != nil {
		t.Skipf("test yığını çalışmıyor (make docker-up): %v", err)
	}
}

// testKey returns the key the container fixture trusts.
func testKey(t *testing.T) []byte {
	t.Helper()

	// The fixture key is generated per machine by docker/make-keys.sh and is
	// never committed.
	data, err := os.ReadFile(filepath.Join("..", "..", "docker", "keys", "id_test"))
	if err != nil {
		t.Skipf("test anahtarı yok (make docker-keys): %v", err)
	}

	return data
}

// TestSetupAuthenticatesAgainstTheStack performs the whole setup into a
// throwaway home directory and then connects with nothing but the files it
// produced.
func TestSetupAuthenticatesAgainstTheStack(t *testing.T) {
	requireStack(t)

	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")

	paths := Paths{
		Dir:        dir,
		Config:     filepath.Join(dir, "config"),
		KnownHosts: filepath.Join(dir, "known_hosts"),
	}

	key, err := WriteKey(t.Context(), paths.Dir, "ns1", testKey(t))
	if err != nil {
		t.Fatalf("anahtar yazılamadı: %v", err)
	}

	scan, err := ScanHost(t.Context(), testHost, testPort)
	if err != nil {
		t.Fatalf("host key alınamadı: %v", err)
	}

	if state, err := CheckKnownHosts(paths.KnownHosts, scan); err != nil {
		t.Fatalf("known_hosts okunamadı: %v", err)
	} else if state != HostUnknown {
		t.Fatalf("boş known_hosts durumu = %v", state)
	}

	if err := AppendKnownHosts(paths.KnownHosts, scan); err != nil {
		t.Fatalf("known_hosts yazılamadı: %v", err)
	}

	if err := UpsertHostBlock(paths.Config, HostEntry{
		Pattern:      testHost,
		HostName:     testHost,
		User:         testUser,
		Port:         testPort,
		IdentityFile: key.Path,
	}); err != nil {
		t.Fatalf("Host bloğu yazılamadı: %v", err)
	}

	// -F names the only configuration file, and BatchMode refuses to fall back
	// to a password, so success proves the key and the host key both came from
	// the files written above.
	cmd := exec.CommandContext(t.Context(), "ssh",
		"-F", paths.Config,
		"-o", "BatchMode=yes",
		"-o", "UserKnownHostsFile="+paths.KnownHosts,
		"-o", "StrictHostKeyChecking=yes",
		"-o", "ConnectTimeout=10",
		testUser+"@"+testHost, "echo yada-ok")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("yazılan ayarlarla bağlanılamadı: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "yada-ok") {
		t.Errorf("uzak komut çıktısı = %q", out)
	}

	// A second setup must be idempotent, since re-adding a server is ordinary.
	if err := UpsertHostBlock(paths.Config, HostEntry{
		Pattern:      testHost,
		HostName:     testHost,
		User:         testUser,
		Port:         testPort,
		IdentityFile: key.Path,
	}); err != nil {
		t.Fatalf("ikinci yazma hatası: %v", err)
	}

	if state, err := CheckKnownHosts(paths.KnownHosts, scan); err != nil {
		t.Fatalf("known_hosts okunamadı: %v", err)
	} else if state != HostMatches {
		t.Errorf("yazıldıktan sonraki durum = %v, HostMatches olmalıydı", state)
	}
}

// A wrong host key must be caught before anything is written, which is the
// whole point of asking about the fingerprint first.
func TestCheckKnownHostsCatchesAWrongEntryForTheStack(t *testing.T) {
	requireStack(t)

	scan, err := ScanHost(t.Context(), testHost, testPort)
	if err != nil {
		t.Fatalf("host key alınamadı: %v", err)
	}

	path := filepath.Join(t.TempDir(), "known_hosts")

	// Same host and port, a key the server does not have.
	line := hostPattern(testHost, testPort) + " ssh-ed25519 " + keyMaterialA + "\n"

	if err := os.WriteFile(path, []byte(line), FileMode); err != nil {
		t.Fatalf("hazırlık yazması başarısız: %v", err)
	}

	// The scan offers several algorithms; only the one the file names has to
	// disagree for this to be a conflict.
	scan.Keys = append(scan.Keys, HostKey{
		Line:        hostPattern(testHost, testPort) + " ssh-ed25519 " + keyMaterialB,
		Type:        "ED25519",
		Fingerprint: "SHA256:sahte",
	})

	state, err := CheckKnownHosts(path, scan)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if state != HostChanged {
		t.Errorf("durum = %v, HostChanged olmalıydı", state)
	}
}
