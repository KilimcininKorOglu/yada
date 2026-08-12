package sshsetup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The ssh configuration is the user's own file. Only the region between this
// tool's own markers is ever rewritten; everything outside them is carried
// over byte for byte.
const (
	markerOpen  = "# >>> yada "
	markerClose = "# <<< yada "
)

// HostEntry is the Host block written for one server.
type HostEntry struct {
	// Pattern is the Host pattern, which is the address the tool will connect
	// to. Using the real address rather than an alias keeps the configuration
	// honest: removing the block falls back to the default keys instead of
	// leaving a name that resolves to nothing.
	Pattern string

	HostName     string
	User         string
	Port         int
	IdentityFile string
}

// ErrForeignHostBlock reports a Host block the user wrote themselves for the
// same pattern. Rewriting it would discard settings this tool knows nothing
// about, and adding a second one would leave ssh applying the first.
var ErrForeignHostBlock = errors.New("ssh ayar dosyasında bu adres için zaten bir Host bloğu var")

// UpsertHostBlock writes the Host block for one server, replacing the block
// this tool wrote for it before.
func UpsertHostBlock(path string, entry HostEntry) error {
	if entry.Pattern == "" {
		return errors.New("host kalıbı boş")
	}

	existing, err := readIfExists(path)
	if err != nil {
		return err
	}

	text := string(existing)

	before, after, found := cutManagedBlock(text, entry.Pattern)
	if !found {
		// Only a block outside our markers counts as foreign; our own was just
		// removed from the text being searched.
		if hasHostPattern(before+after, entry.Pattern) {
			return fmt.Errorf("%w: %s", ErrForeignHostBlock, entry.Pattern)
		}
	}

	if err := ensureDir(dirOf(path)); err != nil {
		return err
	}

	if err := backup(path); err != nil {
		return err
	}

	updated := joinSections(before, renderBlock(entry), after)

	if err := os.WriteFile(path, []byte(updated), FileMode); err != nil {
		return fmt.Errorf("ssh ayar dosyası yazılamadı (%s): %w", path, err)
	}

	return nil
}

// RemoveHostBlock deletes the block this tool wrote for a pattern. A block it
// did not write is left alone.
func RemoveHostBlock(path string, pattern string) error {
	existing, err := readIfExists(path)
	if err != nil || len(existing) == 0 {
		return err
	}

	before, after, found := cutManagedBlock(string(existing), pattern)
	if !found {
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(joinSections(before, "", after)), FileMode); err != nil {
		return fmt.Errorf("ssh ayar dosyası yazılamadı (%s): %w", path, err)
	}

	return nil
}

// cutManagedBlock splits the file around the marked block for a pattern.
func cutManagedBlock(text, pattern string) (before, after string, found bool) {
	open := markerOpen + pattern
	closing := markerClose + pattern

	start := indexOfLine(text, open)
	if start < 0 {
		return text, "", false
	}

	end := indexOfLine(text[start:], closing)
	if end < 0 {
		// An unterminated marker means the file was edited by hand in the
		// middle of our block. Treat it as foreign rather than guess where it
		// ends.
		return text, "", false
	}

	end += start

	// Take the whole closing line with it.
	rest := text[end:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		end += nl + 1
	} else {
		end = len(text)
	}

	return text[:start], text[end:], true
}

// indexOfLine finds a line that begins with prefix, so a mention inside
// another line is not mistaken for a marker.
func indexOfLine(text, prefix string) int {
	offset := 0

	for {
		rest := text[offset:]

		idx := strings.Index(rest, prefix)
		if idx < 0 {
			return -1
		}

		absolute := offset + idx

		if absolute == 0 || text[absolute-1] == '\n' {
			return absolute
		}

		offset = absolute + len(prefix)
	}
}

// hasHostPattern reports whether the file already declares a Host block for a
// pattern.
func hasHostPattern(text, pattern string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Host") {
			continue
		}

		// One Host line may declare several patterns.
		if slices.Contains(fields[1:], pattern) {
			return true
		}
	}

	return false
}

func renderBlock(entry HostEntry) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s%s\n", markerOpen, entry.Pattern)
	fmt.Fprintf(&b, "Host %s\n", entry.Pattern)

	if entry.HostName != "" {
		fmt.Fprintf(&b, "    HostName %s\n", entry.HostName)
	}
	if entry.User != "" {
		fmt.Fprintf(&b, "    User %s\n", entry.User)
	}
	if entry.Port != 0 {
		fmt.Fprintf(&b, "    Port %s\n", strconv.Itoa(entry.Port))
	}

	if entry.IdentityFile != "" {
		fmt.Fprintf(&b, "    IdentityFile %s\n", entry.IdentityFile)
		// Without this a loaded agent offers its own keys first and can use up
		// the server's authentication attempts before this one is tried.
		b.WriteString("    IdentitiesOnly yes\n")
	}

	fmt.Fprintf(&b, "%s%s\n", markerClose, entry.Pattern)

	return b.String()
}

// joinSections puts the file back together with exactly one blank line around
// the block, so repeated edits neither pile up blank lines nor run the block
// into its neighbours.
func joinSections(before, block, after string) string {
	var b strings.Builder

	if trimmed := strings.TrimRight(before, "\n"); trimmed != "" {
		b.WriteString(trimmed)
		b.WriteString("\n\n")
	}

	b.WriteString(block)

	if trimmed := strings.TrimLeft(strings.TrimRight(after, "\n"), "\n"); trimmed != "" {
		if block != "" {
			b.WriteString("\n")
		}

		b.WriteString(trimmed)
		b.WriteString("\n")
	}

	return b.String()
}

func dirOf(path string) string {
	return filepath.Dir(path)
}
