// Package transport runs commands on remote servers through the system ssh
// binary, so the user's own ~/.ssh/config, agent and jump hosts keep working.
package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/KilimcininKorOglu/yada/internal/config"
)

// Result carries everything a remote command produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Success reports whether the remote command exited cleanly.
func (r Result) Success() bool {
	return r.ExitCode == 0
}

// Err turns a non-zero exit into an error carrying the remote stderr. Callers
// that treat a specific non-zero code as meaningful, such as grep returning 1
// for "no match", should inspect ExitCode instead of calling this.
func (r Result) Err() error {
	if r.Success() {
		return nil
	}

	detail := strings.TrimSpace(r.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(r.Stdout)
	}

	if detail == "" {
		return fmt.Errorf("uzak komut %d koduyla başarısız oldu", r.ExitCode)
	}

	return fmt.Errorf("uzak komut %d koduyla başarısız oldu: %s", r.ExitCode, detail)
}

// sshSelfFailureCode is the exit status ssh reserves for its own errors, such
// as a refused connection. A remote command can also return it, so it is a
// hint rather than a guarantee.
const sshSelfFailureCode = 255

// LooksLikeConnectionFailure reports whether the failure most likely came from
// ssh itself rather than from the remote command.
func (r Result) LooksLikeConnectionFailure() bool {
	return r.ExitCode == sshSelfFailureCode
}

// Runner executes commands on a server.
type Runner interface {
	Run(ctx context.Context, srv config.Server, cmd string) (Result, error)
	RunWithStdin(ctx context.Context, srv config.Server, cmd string, stdin io.Reader) (Result, error)
}

// SSHRunner is the production Runner, backed by the system ssh binary.
type SSHRunner struct {
	cfg config.SSH
}

// NewSSHRunner builds a Runner from the ssh section of the configuration.
func NewSSHRunner(cfg config.SSH) *SSHRunner {
	return &SSHRunner{cfg: cfg}
}

// Run executes cmd on srv and returns its output.
func (r *SSHRunner) Run(ctx context.Context, srv config.Server, cmd string) (Result, error) {
	return r.run(ctx, srv, cmd, nil)
}

// RunWithStdin executes cmd on srv, feeding stdin to it. File contents travel
// this way so their bytes never reach the remote command line, where the shell
// would interpret quotes and semicolons.
func (r *SSHRunner) RunWithStdin(ctx context.Context, srv config.Server, cmd string, stdin io.Reader) (Result, error) {
	return r.run(ctx, srv, cmd, stdin)
}

func (r *SSHRunner) run(ctx context.Context, srv config.Server, cmd string, stdin io.Reader) (Result, error) {
	args := r.buildArgs(srv, cmd)

	var stdout, stderr bytes.Buffer

	// No shell is involved: the arguments go straight to execve, so nothing
	// here can be reinterpreted locally. The remote shell does interpret the
	// command, but every value that reaches it is constrained first, in
	// config.validateServer and validateRemotePath, and the record data
	// travels over stdin rather than the command line. gosec cannot follow
	// those checks.
	// #nosec G204
	c := exec.CommandContext(ctx, r.binary(), args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if stdin != nil {
		c.Stdin = stdin
	}

	err := c.Run()

	res := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	switch {
	case err == nil:
		return res, nil

	case ctx.Err() != nil:
		// Cancellation is the caller's own doing, report it as such rather
		// than as a remote failure.
		return res, fmt.Errorf("%s: işlem iptal edildi: %w", srv.Label(), ctx.Err())

	// ErrNotFound covers a failed PATH lookup; an absolute path that does not
	// exist surfaces as fs.ErrNotExist instead.
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return res, fmt.Errorf("%q çalıştırılamadı: %s", r.binary(), missingSSHHint())
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		// The command ran and failed. That is a result, not a transport error.
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}

	return res, fmt.Errorf("%s: ssh çalıştırılamadı: %w", srv.Label(), err)
}

func (r *SSHRunner) binary() string {
	if r.cfg.Binary == "" {
		return "ssh"
	}
	return r.cfg.Binary
}

// buildArgs assembles the ssh argument list. The remote command is always the
// last argument, because ssh treats everything after the destination as the
// command to run.
func (r *SSHRunner) buildArgs(srv config.Server, cmd string) []string {
	args := make([]string, 0, 8+2*len(r.cfg.Options))

	if r.cfg.ConfigFile != "" {
		args = append(args, "-F", r.cfg.ConfigFile)
	}

	// A zero port means the file did not set one, so ssh resolves it from its
	// own configuration.
	if srv.Port > 0 {
		args = append(args, "-p", strconv.Itoa(srv.Port))
	}

	for _, opt := range r.cfg.Options {
		args = append(args, "-o", opt)
	}

	if timeout := r.cfg.ConnectTimeout.Std(); timeout > 0 {
		// ssh only understands whole seconds, and zero would disable the
		// timeout entirely, so anything below a second becomes one.
		seconds := max(int(timeout.Round(time.Second).Seconds()), 1)
		args = append(args, "-o", "ConnectTimeout="+strconv.Itoa(seconds))
	}

	args = append(args, destination(srv), cmd)

	return args
}

func destination(srv config.Server) string {
	if srv.User == "" {
		return srv.Host
	}
	return srv.User + "@" + srv.Host
}

// WithSudo prefixes a command with sudo when the server is configured for it.
func WithSudo(srv config.Server, cmd string) string {
	if !srv.UseSudo() {
		return cmd
	}
	return "sudo " + cmd
}

// Ping checks that the server answers over ssh. BatchMode in the default
// options keeps a password-protected host from blocking on a prompt.
func Ping(ctx context.Context, r Runner, srv config.Server) error {
	res, err := r.Run(ctx, srv, "echo yada-ok")
	if err != nil {
		return err
	}

	if !res.Success() {
		if res.LooksLikeConnectionFailure() {
			return fmt.Errorf("bağlanılamadı: %s", firstLine(res.Stderr))
		}
		return res.Err()
	}

	if !strings.Contains(res.Stdout, "yada-ok") {
		return errors.New("sunucu beklenen yanıtı vermedi")
	}

	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "ayrıntı yok"
	}

	first, _, _ := strings.Cut(s, "\n")
	return first
}

func missingSSHHint() string {
	if runtime.GOOS == "windows" {
		return "OpenSSH istemcisi bulunamadı. Ayarlar > Uygulamalar > İsteğe bağlı özellikler bölümünden OpenSSH Client ekleyin."
	}
	return "ssh istemcisi bulunamadı. Dağıtımınızın openssh-client paketini kurun."
}
