package transport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kerem/unbound-dns/internal/config"
)

func TestBuildArgsPutsCommandLast(t *testing.T) {
	r := NewSSHRunner(config.SSH{
		Options:        []string{"BatchMode=yes"},
		ConnectTimeout: config.Duration(10 * time.Second),
	})

	args := r.buildArgs(config.Server{Host: "10.0.0.1", User: "user01"}, "echo merhaba")

	if got := args[len(args)-1]; got != "echo merhaba" {
		t.Errorf("son argüman = %q, uzak komut olmalıydı", got)
	}
	if got := args[len(args)-2]; got != "user01@10.0.0.1" {
		t.Errorf("hedef = %q", got)
	}
}

func TestBuildArgsOmitsPortWhenUnset(t *testing.T) {
	r := NewSSHRunner(config.SSH{})

	args := r.buildArgs(config.Server{Host: "10.0.0.1", User: "user01"}, "true")

	for _, a := range args {
		if a == "-p" {
			t.Fatalf("port verilmediği halde -p eklendi: %v", args)
		}
	}
}

func TestBuildArgsIncludesExplicitPort(t *testing.T) {
	r := NewSSHRunner(config.SSH{})

	args := r.buildArgs(config.Server{Host: "10.0.0.1", User: "user01", Port: 2222}, "true")

	if !containsPair(args, "-p", "2222") {
		t.Errorf("-p 2222 eklenmedi: %v", args)
	}
}

func TestBuildArgsIncludesOptionsAndTimeout(t *testing.T) {
	r := NewSSHRunner(config.SSH{
		Options:        []string{"BatchMode=yes", "StrictHostKeyChecking=accept-new"},
		ConnectTimeout: config.Duration(7 * time.Second),
		ConfigFile:     "/home/kerem/.ssh/special",
	})

	args := r.buildArgs(config.Server{Host: "h", User: "u"}, "true")

	if !containsPair(args, "-o", "BatchMode=yes") {
		t.Errorf("BatchMode seçeneği eklenmedi: %v", args)
	}
	if !containsPair(args, "-o", "StrictHostKeyChecking=accept-new") {
		t.Errorf("ikinci seçenek eklenmedi: %v", args)
	}
	if !containsPair(args, "-o", "ConnectTimeout=7") {
		t.Errorf("ConnectTimeout eklenmedi: %v", args)
	}
	if !containsPair(args, "-F", "/home/kerem/.ssh/special") {
		t.Errorf("config_file eklenmedi: %v", args)
	}
}

func TestBuildArgsRoundsSubSecondTimeoutUp(t *testing.T) {
	r := NewSSHRunner(config.SSH{ConnectTimeout: config.Duration(200 * time.Millisecond)})

	args := r.buildArgs(config.Server{Host: "h", User: "u"}, "true")

	// ssh only understands whole seconds, and zero would mean "no timeout".
	if !containsPair(args, "-o", "ConnectTimeout=1") {
		t.Errorf("saniye altı zaman aşımı 1'e yuvarlanmadı: %v", args)
	}
}

func TestWithSudo(t *testing.T) {
	on, off := true, false

	if got := WithSudo(config.Server{Sudo: &on}, "systemctl reload unbound"); got != "sudo systemctl reload unbound" {
		t.Errorf("sudo eklenmedi: %q", got)
	}
	if got := WithSudo(config.Server{Sudo: &off}, "systemctl reload unbound"); got != "systemctl reload unbound" {
		t.Errorf("sudo kapalıyken eklendi: %q", got)
	}
	if got := WithSudo(config.Server{}, "true"); got != "true" {
		t.Errorf("sudo tanımsızken eklendi: %q", got)
	}
}

func TestResultErr(t *testing.T) {
	if err := (Result{}).Err(); err != nil {
		t.Errorf("başarılı sonuç hata döndürdü: %v", err)
	}

	err := Result{ExitCode: 1, Stderr: "izin reddedildi"}.Err()
	if err == nil {
		t.Fatal("başarısız sonuç hata döndürmedi")
	}
	if !strings.Contains(err.Error(), "izin reddedildi") {
		t.Errorf("hata stderr içermiyor: %v", err)
	}

	// Some tools report on stdout; the message must not come back empty.
	err = Result{ExitCode: 2, Stdout: "sözdizimi hatası"}.Err()
	if !strings.Contains(err.Error(), "sözdizimi hatası") {
		t.Errorf("stdout yedeği kullanılmadı: %v", err)
	}
}

// The remaining tests drive a stub ssh, so the real argument handling and exit
// codes are exercised without a server.
func TestRunAgainstStub(t *testing.T) {
	requireUnix(t)

	dir := t.TempDir()
	writeStub(t, dir, `#!/bin/sh
# The remote command is the last argument.
for cmd; do :; done
printf 'gelen: %s\n' "$cmd"
exit 0
`)

	r := NewSSHRunner(config.SSH{Binary: filepath.Join(dir, "ssh")})

	res, err := r.Run(context.Background(), config.Server{Host: "h", User: "u"}, "unbound-checkconf")
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if !res.Success() {
		t.Errorf("çıkış kodu = %d", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "gelen: unbound-checkconf") {
		t.Errorf("uzak komut son argüman olarak gitmedi: %q", res.Stdout)
	}
}

func TestRunReportsExitCodeWithoutError(t *testing.T) {
	requireUnix(t)

	dir := t.TempDir()
	writeStub(t, dir, `#!/bin/sh
echo "sözdizimi hatası" >&2
exit 3
`)

	r := NewSSHRunner(config.SSH{Binary: filepath.Join(dir, "ssh")})

	res, err := r.Run(context.Background(), config.Server{Host: "h", User: "u"}, "true")
	if err != nil {
		t.Fatalf("başarısız komut transport hatası verdi: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("çıkış kodu = %d, beklenen 3", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "sözdizimi hatası") {
		t.Errorf("stderr yakalanmadı: %q", res.Stderr)
	}
}

func TestRunWithStdinFeedsRemoteCommand(t *testing.T) {
	requireUnix(t)

	dir := t.TempDir()
	writeStub(t, dir, `#!/bin/sh
cat
`)

	r := NewSSHRunner(config.SSH{Binary: filepath.Join(dir, "ssh")})

	// Content that would break if it were interpolated into a shell command.
	payload := `local-data: "mail.example.com. IN A 10.0.0.1"; rm -rf /`

	res, err := r.RunWithStdin(context.Background(), config.Server{Host: "h", User: "u"},
		"tee /etc/unbound/local_records.conf", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if res.Stdout != payload {
		t.Errorf("stdin bozuldu:\ngelen  = %q\nbeklenen = %q", res.Stdout, payload)
	}
}

func TestRunReportsMissingBinary(t *testing.T) {
	r := NewSSHRunner(config.SSH{Binary: filepath.Join(t.TempDir(), "yok-boyle-bir-ssh")})

	_, err := r.Run(context.Background(), config.Server{Host: "h", User: "u"}, "true")
	if err == nil {
		t.Fatal("eksik ssh binary'si hata vermedi")
	}
	if !strings.Contains(err.Error(), "bulunamadı") {
		t.Errorf("hata kurulum yönlendirmesi içermiyor: %v", err)
	}
}

func TestRunHonoursContextCancellation(t *testing.T) {
	requireUnix(t)

	dir := t.TempDir()
	writeStub(t, dir, `#!/bin/sh
sleep 30
`)

	r := NewSSHRunner(config.SSH{Binary: filepath.Join(dir, "ssh")})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.Run(ctx, config.Server{Host: "h", User: "u"}, "true")

	if err == nil {
		t.Fatal("iptal edilen komut hata vermedi")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("hata = %v, DeadlineExceeded sarmalanmalıydı", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("iptal alt süreci öldürmedi, %s bekledi", elapsed)
	}
}

func TestPingSucceeds(t *testing.T) {
	requireUnix(t)

	dir := t.TempDir()
	writeStub(t, dir, `#!/bin/sh
for cmd; do :; done
exec /bin/sh -c "$cmd"
`)

	r := NewSSHRunner(config.SSH{Binary: filepath.Join(dir, "ssh")})

	if err := Ping(context.Background(), r, config.Server{Host: "h", User: "u"}); err != nil {
		t.Errorf("beklenmeyen hata: %v", err)
	}
}

func TestPingReportsConnectionFailure(t *testing.T) {
	requireUnix(t)

	dir := t.TempDir()
	writeStub(t, dir, `#!/bin/sh
echo "ssh: connect to host h port 22: Connection refused" >&2
exit 255
`)

	r := NewSSHRunner(config.SSH{Binary: filepath.Join(dir, "ssh")})

	err := Ping(context.Background(), r, config.Server{Host: "h", User: "u"})
	if err == nil {
		t.Fatal("bağlantı hatası raporlanmadı")
	}
	if !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("hata ssh çıktısını göstermiyor: %v", err)
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func writeStub(t *testing.T, dir, body string) {
	t.Helper()

	path := filepath.Join(dir, "ssh")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("sahte ssh yazılamadı: %v", err)
	}
}

func requireUnix(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("sahte ssh betiği POSIX kabuk gerektirir")
	}
}
