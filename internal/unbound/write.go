package unbound

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
)

// BackupSuffix is appended to the records file path for the rollback copy.
// Only the last write is kept: timestamped backups would grow without bound on
// a server nobody prunes.
const BackupSuffix = ".bak"

// WriteOptions controls a single write.
type WriteOptions struct {
	// Backup takes a copy before writing, which is what makes a rollback
	// possible when validation fails.
	Backup bool

	// DryRun computes and reports the change without touching the server.
	DryRun bool
}

// WriteResult reports what happened on one server.
type WriteResult struct {
	Server config.Server

	// Changed is true when the file on the server was actually modified.
	Changed bool

	// RolledBack is true when validation failed and the backup was restored.
	RolledBack bool

	// CheckOutput holds the unbound-checkconf output when validation failed.
	CheckOutput string

	// Diff lists the added and removed lines, and is filled for a dry run too.
	Diff Diff

	Err error
}

// Diff is a line-level summary of a pending change.
type Diff struct {
	Added   []string
	Removed []string
}

// Empty reports whether the change is a no-op.
func (d Diff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0
}

// String renders the diff for display.
func (d Diff) String() string {
	var b strings.Builder

	for _, line := range d.Removed {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(line))
	}
	for _, line := range d.Added {
		fmt.Fprintf(&b, "+ %s\n", strings.TrimSpace(line))
	}

	return b.String()
}

// DiffContent compares two file bodies line by line. Order is ignored, because
// the writer only ever appends and removes whole lines; what matters is which
// lines came and went.
func DiffContent(before, after []byte) Diff {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)

	beforeCount := countLines(beforeLines)
	afterCount := countLines(afterLines)

	var d Diff

	for _, line := range afterLines {
		if beforeCount[line] > 0 {
			beforeCount[line]--
			continue
		}
		d.Added = append(d.Added, line)
	}

	for _, line := range beforeLines {
		if afterCount[line] > 0 {
			afterCount[line]--
			continue
		}
		d.Removed = append(d.Removed, line)
	}

	return d
}

func splitLines(data []byte) []string {
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}

	return strings.Split(text, "\n")
}

func countLines(lines []string) map[string]int {
	counts := make(map[string]int, len(lines))
	for _, line := range lines {
		counts[line]++
	}

	return counts
}

// Write applies a modified file to a server: back up, write, validate, and
// roll back if validation fails. The service is never touched here; refreshing
// is a separate step that only runs once the config is known to be good.
func Write(ctx context.Context, r transport.Runner, srv config.Server, before []byte, file *records.File, opts WriteOptions) WriteResult {
	res := WriteResult{Server: srv}

	after := file.Bytes()
	res.Diff = DiffContent(before, after)

	if res.Diff.Empty() {
		return res
	}

	if opts.DryRun {
		return res
	}

	backupPath := srv.RecordsFile + BackupSuffix

	if opts.Backup {
		if err := copyRemote(ctx, r, srv, srv.RecordsFile, backupPath); err != nil {
			res.Err = fmt.Errorf("yedek alınamadı, yazma yapılmadı: %w", err)
			return res
		}
	}

	if err := writeRemote(ctx, r, srv, after); err != nil {
		res.Err = err

		// The write may have left the file half-written, so restore it even
		// though validation never ran.
		if opts.Backup {
			res.RolledBack, res.Err = rollback(ctx, r, srv, backupPath, res.Err)
		}

		return res
	}

	res.Changed = true

	valid, output := probeConfig(ctx, r, srv)
	res.CheckOutput = output

	if valid {
		return res
	}

	res.Err = fmt.Errorf("config doğrulaması başarısız")

	if !opts.Backup {
		res.Err = fmt.Errorf(
			"%w ve yedek kapalı olduğu için geri alınamadı, %s dosyasını elle düzeltin",
			res.Err, srv.RecordsFile)

		return res
	}

	res.RolledBack, res.Err = rollback(ctx, r, srv, backupPath, res.Err)
	if res.RolledBack {
		res.Changed = false
	}

	return res
}

// rollback restores the backup. A failure here is the worst case: the server
// is left with a config it rejects, so the message has to name the backup file
// the operator needs.
func rollback(ctx context.Context, r transport.Runner, srv config.Server, backupPath string, cause error) (bool, error) {
	if err := copyRemote(ctx, r, srv, backupPath, srv.RecordsFile); err != nil {
		return false, fmt.Errorf(
			"%w; ÜSTELİK geri alma da başarısız oldu (%v), sunucudaki %s dosyası bozuk durumda, yedek: %s",
			cause, err, srv.RecordsFile, backupPath)
	}

	return true, cause
}

func copyRemote(ctx context.Context, r transport.Runner, srv config.Server, from, to string) error {
	// -p keeps mode and ownership, so restoring a backup cannot silently
	// change who may read the records file.
	cmd := transport.WithSudo(srv, fmt.Sprintf("cp -p %s %s", from, to))

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return err
	}

	if !res.Success() {
		return res.Err()
	}

	return nil
}

// writeRemote streams the new content over stdin. Passing it as a command
// argument would hand the remote shell every quote and semicolon in the
// records to interpret.
func writeRemote(ctx context.Context, r transport.Runner, srv config.Server, content []byte) error {
	cmd := transport.WithSudo(srv, "tee "+srv.RecordsFile+" > /dev/null")

	res, err := r.RunWithStdin(ctx, srv, cmd, bytes.NewReader(content))
	if err != nil {
		return err
	}

	if !res.Success() {
		return fmt.Errorf("%s yazılamadı: %w", srv.RecordsFile, res.Err())
	}

	return nil
}

// Apply reads, modifies and writes the records file on every server. The
// mutate callback receives each server's own parsed file, so a change is
// applied to what that server actually has rather than to a shared copy.
//
// The callback may run concurrently for different servers, so it must not
// write to state shared between them.
func Apply(
	ctx context.Context,
	r transport.Runner,
	cfg config.Config,
	opts WriteOptions,
	mutate func(*records.File) error,
) []WriteResult {
	return ApplyPerServer(ctx, r, cfg, opts, func(_ config.Server, f *records.File) error {
		return mutate(f)
	})
}

// ApplyPerServer is Apply with the server passed to the callback, for changes
// that differ per server such as a sync plan.
func ApplyPerServer(
	ctx context.Context,
	r transport.Runner,
	cfg config.Config,
	opts WriteOptions,
	mutate func(config.Server, *records.File) error,
) []WriteResult {
	return ForEachServer(ctx, cfg, func(ctx context.Context, srv config.Server) WriteResult {
		file, err := ReadFile(ctx, r, srv)
		if err != nil {
			return WriteResult{Server: srv, Err: err}
		}

		before := file.Bytes()

		if err := mutate(srv, file); err != nil {
			return WriteResult{Server: srv, Err: err}
		}

		return Write(ctx, r, srv, before, file, opts)
	})
}
