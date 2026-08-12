package unbound

import (
	"context"
	"fmt"
	"strings"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
)

// pushLocalData installs a record change in the running daemon with
// unbound-control, without re-reading the configuration file.
//
// This is the cheapest way to make a change take effect: nothing is reparsed,
// no cache entry is dropped and the daemon never stops answering. It is only
// usable when the exact set of changed records is known, which is why it is
// reserved for a refresh that follows a write this tool performed. Persistence
// still comes from the file that was written first; this only aligns the
// running state with it.
func pushLocalData(ctx context.Context, r transport.Runner, srv config.Server, change records.Change) (string, error) {
	if change.Empty() {
		return "", fmt.Errorf("değişiklik kümesi bilinmiyor, local_data kullanılamaz")
	}

	var output []string

	// Removals run first. local_data_remove wipes every type registered under
	// a name, so the records the file still holds for those names are pushed
	// back in the add step below.
	if len(change.Removed) > 0 {
		names := uniqueNames(change.Removed)

		out, err := controlWithStdin(ctx, r, srv, "local_datas_remove", strings.Join(names, "\n"))
		if err != nil {
			return out, err
		}

		output = append(output, describeCount("kaldırılan ad", len(names), out))
	}

	if len(change.ZonesRemoved) > 0 {
		out, err := controlWithStdin(ctx, r, srv, "local_zones_remove", strings.Join(change.ZonesRemoved, "\n"))
		if err != nil {
			return out, err
		}

		output = append(output, describeCount("kaldırılan zone", len(change.ZonesRemoved), out))
	}

	// Added and Retained go in one batch: both are records the file holds and
	// the daemon must have. local_data creates a covering transparent zone by
	// itself when none exists, which matches what the file declares.
	install := append(append([]records.Record{}, change.Added...), change.Retained...)

	if len(install) > 0 {
		lines := make([]string, 0, len(install))
		for _, rec := range install {
			lines = append(lines, rec.String())
		}

		out, err := controlWithStdin(ctx, r, srv, "local_datas", strings.Join(lines, "\n"))
		if err != nil {
			return out, err
		}

		output = append(output, describeCount("yazılan kayıt", len(lines), out))
	}

	return strings.Join(output, "; "), nil
}

// controlWithStdin runs an unbound-control subcommand that reads its input from
// standard input. Feeding the records this way keeps their quotes and
// semicolons away from the remote shell.
func controlWithStdin(ctx context.Context, r transport.Runner, srv config.Server, subcommand, input string) (string, error) {
	cmd := transport.WithSudo(srv, "unbound-control "+subcommand+" 2>&1")

	res, err := r.RunWithStdin(ctx, srv, cmd, strings.NewReader(input+"\n"))
	if err != nil {
		return "", err
	}

	out := strings.TrimSpace(res.Stdout)

	if !res.Success() {
		return out, fmt.Errorf("unbound-control %s başarısız (çıkış kodu %d)", subcommand, res.ExitCode)
	}

	return out, nil
}

func describeCount(label string, count int, remoteOutput string) string {
	text := fmt.Sprintf("%d %s", count, label)

	// unbound-control answers "ok" on success, which adds nothing. Anything
	// else is worth showing.
	if remoteOutput != "" && remoteOutput != "ok" {
		text += " (" + remoteOutput + ")"
	}

	return text
}

func uniqueNames(recs []records.Record) []string {
	seen := make(map[string]bool, len(recs))
	out := make([]string, 0, len(recs))

	for _, rec := range recs {
		if seen[rec.Name] {
			continue
		}

		seen[rec.Name] = true
		out = append(out, rec.Name)
	}

	return out
}

// ListLocalData reads the local data the daemon currently holds in memory. It
// is what makes a runtime push verifiable: the file says one thing, this says
// what the resolver actually answers with.
func ListLocalData(ctx context.Context, r transport.Runner, srv config.Server) ([]records.Record, error) {
	cmd := transport.WithSudo(srv, "unbound-control list_local_data")

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return nil, err
	}

	if !res.Success() {
		return nil, fmt.Errorf("unbound-control list_local_data başarısız: %w", res.Err())
	}

	var out []records.Record

	for line := range strings.SplitSeq(res.Stdout, "\n") {
		rec, ok := records.ParseWireLine(line)
		if !ok {
			continue
		}

		out = append(out, rec)
	}

	return out, nil
}
