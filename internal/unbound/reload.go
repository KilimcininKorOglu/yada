package unbound

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
)

// Tier identifies how the daemon was told to pick up the new config.
type Tier string

const (
	// TierLocalData is unbound-control local_datas: the changed records are
	// pushed straight into the running daemon. Nothing is reparsed and no cache
	// entry is dropped, so it is the cheapest tier. It needs the exact change
	// set, so it only applies to a refresh that follows a write.
	TierLocalData Tier = "local_data"
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
	case TierLocalData:
		return "kesinti yok, config okunmadı, cache korundu"
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

// Reload refreshes one server without knowing what changed, so the runtime
// push tier is unavailable and the daemon has to re-read its configuration.
func Reload(ctx context.Context, r transport.Runner, srv config.Server, strategy config.ReloadStrategy) ReloadResult {
	return Refresh(ctx, r, srv, strategy, records.Change{})
}

// Refresh makes a change take effect on one server, walking the tiers from
// lightest to heaviest.
//
// The change is the set of records the write moved. When it is known the
// daemon can be updated in place with unbound-control local_data, which is why
// a refresh that follows a write is cheaper than a bare reload. The configured
// strategy can pin a single tier, in which case no fallback happens and a
// failure is reported as such.
func Refresh(
	ctx context.Context,
	r transport.Runner,
	srv config.Server,
	strategy config.ReloadStrategy,
	change records.Change,
) ReloadResult {
	res := ReloadResult{Server: srv}

	for _, tier := range tiersFor(strategy, !change.Empty()) {
		output, err := runTier(ctx, r, srv, tier, change)

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
//
// The local_data tier is only offered when the change set is known. Pinning it
// without one is a configuration the caller cannot satisfy, so the strategy
// falls back to re-reading the config rather than failing outright.
func tiersFor(strategy config.ReloadStrategy, haveChange bool) []Tier {
	switch strategy {
	case config.ReloadLocalData:
		if haveChange {
			return []Tier{TierLocalData}
		}
		return []Tier{TierControl}
	case config.ReloadControl:
		return []Tier{TierControl}
	case config.ReloadSignal:
		return []Tier{TierSignal}
	case config.ReloadRestart:
		return []Tier{TierRestart}
	}

	if haveChange {
		return []Tier{TierLocalData, TierControl, TierSignal, TierRestart}
	}

	return []Tier{TierControl, TierSignal, TierRestart}
}

func runTier(
	ctx context.Context,
	r transport.Runner,
	srv config.Server,
	tier Tier,
	change records.Change,
) (string, error) {
	switch tier {
	case TierLocalData:
		return pushLocalData(ctx, r, srv, change)
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

// ReloadAll refreshes every configured server without a known change set.
func ReloadAll(ctx context.Context, r transport.Runner, cfg config.Config) []ReloadResult {
	return RefreshAll(ctx, r, cfg, nil)
}

// RefreshAll refreshes every configured server, using each one's own change
// when the caller knows it. The map is keyed by server label, since the same
// write can move different records on different servers.
func RefreshAll(
	ctx context.Context,
	r transport.Runner,
	cfg config.Config,
	changes map[string]records.Change,
) []ReloadResult {
	return ForEachServer(ctx, cfg, func(ctx context.Context, srv config.Server) ReloadResult {
		return Refresh(ctx, r, srv, cfg.Behaviour.ReloadStrategy, changes[srv.Label()])
	})
}

// ChangesOf collects the record-level change of every write that touched a
// server, ready to hand to RefreshAll.
func ChangesOf(results []WriteResult) map[string]records.Change {
	changes := make(map[string]records.Change, len(results))

	for _, res := range results {
		if res.Changed {
			changes[res.Server.Label()] = res.Change
		}
	}

	return changes
}

// ChangedServers lists the servers whose file the write actually modified.
// A server that was skipped, failed or ran under --dry-run has nothing new to
// pick up, so refreshing it would be pointless work.
func ChangedServers(results []WriteResult) []config.Server {
	var out []config.Server

	for _, res := range results {
		if res.Changed {
			out = append(out, res.Server)
		}
	}

	return out
}

// RefreshWrites refreshes exactly the servers a write changed, pushing each
// one's own record change into its daemon.
func RefreshWrites(
	ctx context.Context,
	r transport.Runner,
	cfg config.Config,
	results []WriteResult,
) []ReloadResult {
	changed := ChangedServers(results)
	if len(changed) == 0 {
		return nil
	}

	scoped := cfg
	scoped.Servers = changed

	return RefreshAll(ctx, r, scoped, ChangesOf(results))
}
