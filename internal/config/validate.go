package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Remote paths always use POSIX separators, so filepath.IsAbs cannot be used:
// on Windows it rejects "/etc/unbound/unbound.conf". These patterns also keep
// shell metacharacters out of the values that reach the remote command line.
var (
	remotePathPattern = regexp.MustCompile(`^/[A-Za-z0-9/._-]*$`)
	hostPattern       = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	userPattern       = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Validate reports every problem found in the configuration at once, so the
// user fixes them in a single pass instead of one error per run.
func (c Config) Validate() error {
	var problems []error

	if len(c.Servers) == 0 {
		problems = append(problems, errors.New("en az bir sunucu tanımlanmalı (servers listesi boş)"))
	}

	// A server is identified by host and port together. The same host on two
	// ports is a different machine whenever ssh forwarding or containers are
	// involved, so keying on the host alone would reject a valid setup.
	seen := make(map[string]int, len(c.Servers))

	for i, srv := range c.Servers {
		where := fmt.Sprintf("servers[%d]", i)
		if srv.Name != "" {
			where = fmt.Sprintf("servers[%d] (%s)", i, srv.Name)
		}

		problems = append(problems, validateServer(where, srv)...)

		if srv.Host == "" {
			continue
		}

		key := fmt.Sprintf("%s:%d", srv.Host, srv.effectivePort(c.Defaults.Port))

		if first, dup := seen[key]; dup {
			problems = append(problems, fmt.Errorf(
				"%s: %s zaten servers[%d] içinde tanımlı", where, describeTarget(srv, c.Defaults.Port), first))
		} else {
			seen[key] = i
		}
	}

	problems = append(problems, validateSSH(c.SSH)...)
	problems = append(problems, validateBehaviour(c.Behaviour)...)
	problems = append(problems, validateLog(c.Log)...)

	return errors.Join(problems...)
}

// effectivePort resolves the port a server will actually use. Zero means ssh
// picks it, which for duplicate detection is one more distinct value.
func (s Server) effectivePort(defaultPort int) int {
	if s.Port != 0 {
		return s.Port
	}

	return defaultPort
}

func describeTarget(srv Server, defaultPort int) string {
	if port := srv.effectivePort(defaultPort); port != 0 {
		return fmt.Sprintf("host %q ve port %d", srv.Host, port)
	}

	return fmt.Sprintf("host %q", srv.Host)
}

func validateServer(where string, srv Server) []error {
	var problems []error

	switch {
	case srv.Host == "":
		problems = append(problems, fmt.Errorf("%s: host alanı zorunlu", where))
	case !hostPattern.MatchString(srv.Host):
		problems = append(problems, fmt.Errorf("%s: host %q geçersiz karakter içeriyor", where, srv.Host))
	}

	switch {
	case srv.User == "":
		problems = append(problems, fmt.Errorf("%s: user alanı zorunlu (sunucuda veya defaults içinde tanımlayın)", where))
	case !userPattern.MatchString(srv.User):
		problems = append(problems, fmt.Errorf("%s: user %q geçersiz karakter içeriyor", where, srv.User))
	}

	// Zero means the port was not set, and ssh resolves it itself.
	if srv.Port != 0 && (srv.Port < 1 || srv.Port > 65535) {
		problems = append(problems, fmt.Errorf("%s: port %d aralık dışında (1-65535)", where, srv.Port))
	}

	problems = append(problems, validateRemotePath(where, "records_file", srv.RecordsFile)...)
	problems = append(problems, validateRemotePath(where, "main_config", srv.MainConfig)...)

	if srv.RecordsFile != "" && srv.RecordsFile == srv.MainConfig {
		problems = append(problems, fmt.Errorf("%s: records_file ve main_config aynı dosyayı gösteriyor", where))
	}

	return problems
}

func validateRemotePath(where, field, value string) []error {
	switch {
	case value == "":
		return []error{fmt.Errorf("%s: %s alanı zorunlu (sunucuda veya defaults içinde tanımlayın)", where, field)}
	case !strings.HasPrefix(value, "/"):
		return []error{fmt.Errorf("%s: %s mutlak yol olmalı, %q verilmiş", where, field, value)}
	case !remotePathPattern.MatchString(value):
		return []error{fmt.Errorf("%s: %s %q izin verilmeyen karakter içeriyor (harf, rakam, / . _ - kullanın)", where, field, value)}
	}

	return nil
}

func validateSSH(s SSH) []error {
	var problems []error

	if strings.TrimSpace(s.Binary) == "" {
		problems = append(problems, errors.New("ssh.binary boş olamaz"))
	}

	if s.ConnectTimeout.Std() <= 0 {
		problems = append(problems, fmt.Errorf("ssh.connect_timeout pozitif olmalı, %s verilmiş", s.ConnectTimeout.Std()))
	}

	for i, opt := range s.Options {
		if !strings.Contains(opt, "=") {
			problems = append(problems, fmt.Errorf("ssh.options[%d]: %q anahtar=değer biçiminde olmalı", i, opt))
		}
	}

	return problems
}

func validateBehaviour(b Behaviour) []error {
	var problems []error

	switch b.ReloadStrategy {
	case ReloadAuto, ReloadLocalData, ReloadControl, ReloadSignal, ReloadRestart:
	default:
		problems = append(problems, fmt.Errorf(
			"behaviour.reload_strategy %q bilinmiyor (auto, local_data, control, signal, restart)", b.ReloadStrategy))
	}

	if b.MaxParallel < 1 {
		problems = append(problems, fmt.Errorf("behaviour.max_parallel en az 1 olmalı, %d verilmiş", b.MaxParallel))
	}

	return problems
}

func validateLog(l Log) []error {
	switch l.Level {
	case LogDebug, LogInfo, LogWarn, LogError:
		return nil
	default:
		return []error{fmt.Errorf("log.level %q bilinmiyor (debug, info, warn, error)", l.Level)}
	}
}
