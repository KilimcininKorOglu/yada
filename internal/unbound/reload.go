package unbound

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/transport"
)

// Tier identifies how the daemon was told to pick up the new config.
type Tier string

const (
	// TierControl is unbound-control reload_keep_cache: no outage, cache kept.
	TierControl Tier = "reload_keep_cache"
	// TierSignal is systemctl reload, which delivers SIGHUP: no outage, cache
	// flushed, and no control channel needed.
	TierSignal Tier = "systemctl reload"
	// TierRestart is systemctl restart: brief outage, cache flushed.
	TierRestart Tier = "systemctl restart"
)

// Description explains the cost of a tier in user-facing terms.
func (t Tier) Description() string {
	switch t {
	case TierControl:
		return "kesinti yok, cache korundu"
	case TierSignal:
		return "kesinti yok, cache temizlendi"
	case TierRestart:
		return "servis yeniden başlatıldı, cache temizlendi"
	default:
		return ""
	}
}

// activePollInterval and activePollAttempts bound the wait for the service to
// come back after a restart. They are variables so tests can shorten the wait
// instead of spending half a minute per restart case.
var (
	activePollInterval = 2 * time.Second
	activePollAttempts = 15
)

// ReloadResult reports how a single server was refreshed.
type ReloadResult struct {
	Server config.Server

	// Tier is the one that succeeded.
	Tier Tier

	// Attempts records every tier tried and why it failed, so the user can see
	// what made the tool fall through to a heavier option.
	Attempts []TierAttempt

	Err error
}

// TierAttempt is one tier that was tried.
type TierAttempt struct {
	Tier   Tier
	Err    error
	Output string
}

// Reload refreshes one server, walking the tiers from lightest to heaviest.
// The configured strategy can pin a single tier, in which case no fallback
// happens and a failure is reported as such.
func Reload(ctx context.Context, r transport.Runner, srv config.Server, strategy config.ReloadStrategy) ReloadResult {
	res := ReloadResult{Server: srv}

	for _, tier := range tiersFor(strategy) {
		output, err := runTier(ctx, r, srv, tier)

		res.Attempts = append(res.Attempts, TierAttempt{Tier: tier, Err: err, Output: output})

		if err == nil {
			res.Tier = tier
			return res
		}
	}

	res.Err = fmt.Errorf("hiçbir yenileme yöntemi çalışmadı")

	return res
}

// tiersFor maps the configured strategy to the tiers to attempt, in order.
func tiersFor(strategy config.ReloadStrategy) []Tier {
	switch strategy {
	case config.ReloadControl:
		return []Tier{TierControl}
	case config.ReloadSignal:
		return []Tier{TierSignal}
	case config.ReloadRestart:
		return []Tier{TierRestart}
	default:
		return []Tier{TierControl, TierSignal, TierRestart}
	}
}

func runTier(ctx context.Context, r transport.Runner, srv config.Server, tier Tier) (string, error) {
	switch tier {
	case TierControl:
		return reloadControl(ctx, r, srv)
	case TierSignal:
		return reloadSignal(ctx, r, srv)
	case TierRestart:
		return restartService(ctx, r, srv)
	default:
		return "", fmt.Errorf("bilinmeyen yenileme kademesi %q", tier)
	}
}

func reloadControl(ctx context.Context, r transport.Runner, srv config.Server) (string, error) {
	cmd := transport.WithSudo(srv, "unbound-control reload_keep_cache 2>&1")

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return "", err
	}

	output := strings.TrimSpace(res.Stdout)

	if !res.Success() {
		return output, fmt.Errorf("unbound-control kullanılamadı (çıkış kodu %d)", res.ExitCode)
	}

	return output, nil
}

// reloadSignal uses systemctl reload, which sends SIGHUP. Unbound re-reads its
// config on that signal, so this works without the control channel.
func reloadSignal(ctx context.Context, r transport.Runner, srv config.Server) (string, error) {
	cmd := transport.WithSudo(srv, "systemctl reload unbound 2>&1")

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return "", err
	}

	output := strings.TrimSpace(res.Stdout)

	if !res.Success() {
		return output, fmt.Errorf("systemctl reload başarısız (çıkış kodu %d)", res.ExitCode)
	}

	// A config the daemon rejects while re-reading kills the process, and
	// systemctl reload still exits zero, so the state has to be confirmed.
	if !probeServiceActive(ctx, r, srv) {
		return output, errors.New("reload sonrası servis aktif değil")
	}

	return output, nil
}

func restartService(ctx context.Context, r transport.Runner, srv config.Server) (string, error) {
	cmd := transport.WithSudo(srv, "systemctl restart unbound 2>&1")

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return "", err
	}

	output := strings.TrimSpace(res.Stdout)

	if !res.Success() {
		return output, fmt.Errorf("systemctl restart başarısız (çıkış kodu %d)", res.ExitCode)
	}

	if err := waitForActive(ctx, r, srv); err != nil {
		return output, err
	}

	return output, nil
}

// waitForActive polls until the service reports active or the attempts run
// out. A restart is not instantaneous, so a single immediate check would
// report failure on a server that is merely still starting.
func waitForActive(ctx context.Context, r transport.Runner, srv config.Server) error {
	for range activePollAttempts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(activePollInterval):
		}

		if probeServiceActive(ctx, r, srv) {
			return nil
		}
	}

	return fmt.Errorf("servis %s içinde aktif duruma gelmedi",
		time.Duration(activePollAttempts)*activePollInterval)
}

// ReloadAll refreshes every configured server.
func ReloadAll(ctx context.Context, r transport.Runner, cfg config.Config) []ReloadResult {
	return ForEachServer(ctx, cfg, func(ctx context.Context, srv config.Server) ReloadResult {
		return Reload(ctx, r, srv, cfg.Behaviour.ReloadStrategy)
	})
}
