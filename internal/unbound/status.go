// Package unbound talks to the Unbound daemon and its configuration on a
// remote server.
package unbound

import (
	"context"
	"strings"
	"sync"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/transport"
)

// Status is the outcome of inspecting one server.
type Status struct {
	Server config.Server

	// Reachable reports whether ssh answered at all. When it is false every
	// field below is meaningless and Err explains why.
	Reachable bool

	// ConfigValid is the unbound-checkconf verdict for the main config.
	ConfigValid  bool
	ConfigOutput string

	// ControlAvailable reports whether unbound-control can talk to the daemon,
	// which decides if reload_keep_cache is usable.
	ControlAvailable bool
	ControlOutput    string

	// SignalReloadAvailable reports whether the unit file defines ExecReload,
	// which decides if a SIGHUP reload is usable.
	SignalReloadAvailable bool

	ServiceActive bool
	Version       string

	Err error
}

// AvailableTier names the lightest refresh tier this server supports.
func (s Status) AvailableTier() string {
	switch {
	case !s.Reachable:
		return "yok"
	case s.ControlAvailable:
		return "reload_keep_cache"
	case s.SignalReloadAvailable:
		return "systemctl reload"
	default:
		return "systemctl restart"
	}
}

// Check inspects a single server. It never returns an error: a failure is
// recorded in the Status so one unreachable host does not hide the others.
func Check(ctx context.Context, r transport.Runner, srv config.Server) Status {
	st := Status{Server: srv}

	if err := transport.Ping(ctx, r, srv); err != nil {
		st.Err = err
		return st
	}
	st.Reachable = true

	st.Version = probeVersion(ctx, r, srv)
	st.ServiceActive = probeServiceActive(ctx, r, srv)
	st.ConfigValid, st.ConfigOutput = probeConfig(ctx, r, srv)
	st.ControlAvailable, st.ControlOutput = probeControl(ctx, r, srv)
	st.SignalReloadAvailable = probeSignalReload(ctx, r, srv)

	return st
}

// CheckAll inspects every configured server, honouring the parallelism
// settings.
func CheckAll(ctx context.Context, r transport.Runner, cfg config.Config) []Status {
	return ForEachServer(ctx, cfg, func(ctx context.Context, srv config.Server) Status {
		return Check(ctx, r, srv)
	})
}

// ForEachServer applies fn to every server, in parallel when the configuration
// asks for it. Results keep the order servers are declared in, so output does
// not shuffle between runs.
func ForEachServer[T any](ctx context.Context, cfg config.Config, fn func(context.Context, config.Server) T) []T {
	results := make([]T, len(cfg.Servers))

	if !cfg.Behaviour.Parallel || len(cfg.Servers) < 2 {
		for i, srv := range cfg.Servers {
			results[i] = fn(ctx, srv)
		}
		return results
	}

	limit := max(cfg.Behaviour.MaxParallel, 1)

	var wg sync.WaitGroup
	slots := make(chan struct{}, limit)

	for i, srv := range cfg.Servers {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			results[i] = fn(ctx, srv)
		})
	}

	wg.Wait()

	return results
}

func probeVersion(ctx context.Context, r transport.Runner, srv config.Server) string {
	res, err := r.Run(ctx, srv, "unbound -V 2>&1 | head -1")
	if err != nil || !res.Success() {
		return ""
	}

	return strings.TrimSpace(res.Stdout)
}

func probeServiceActive(ctx context.Context, r transport.Runner, srv config.Server) bool {
	res, err := r.Run(ctx, srv, transport.WithSudo(srv, "systemctl is-active unbound"))
	if err != nil {
		return false
	}

	return strings.TrimSpace(res.Stdout) == "active"
}

// probeConfig validates the main config. The fragment holding the records
// cannot be checked on its own, because include splices it into the server
// clause and it carries no clause of its own.
func probeConfig(ctx context.Context, r transport.Runner, srv config.Server) (bool, string) {
	cmd := transport.WithSudo(srv, "unbound-checkconf "+srv.MainConfig+" 2>&1")

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return false, err.Error()
	}

	return res.Success(), strings.TrimSpace(res.Stdout)
}

func probeControl(ctx context.Context, r transport.Runner, srv config.Server) (bool, string) {
	cmd := transport.WithSudo(srv, "unbound-control status 2>&1")

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return false, err.Error()
	}

	if res.Success() {
		return true, ""
	}

	return false, strings.TrimSpace(res.Stdout)
}

// probeSignalReload asks systemd whether the unit defines ExecReload. Without
// it, systemctl reload fails and only a restart is left.
func probeSignalReload(ctx context.Context, r transport.Runner, srv config.Server) bool {
	res, err := r.Run(ctx, srv, "systemctl show unbound --property=CanReload --value 2>/dev/null")
	if err != nil || !res.Success() {
		return false
	}

	return strings.TrimSpace(res.Stdout) == "yes"
}
