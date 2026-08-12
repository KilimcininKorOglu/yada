package sshsetup

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// HostKey is one key a server offered, with the fingerprint to show the user.
type HostKey struct {
	// Line is the known_hosts entry exactly as ssh-keyscan produced it.
	Line string

	// Type is the key algorithm, for example "ED25519".
	Type string

	// Fingerprint is the SHA256 form ssh prints when it asks about a new host.
	Fingerprint string
}

// Scan is everything a server offered.
type Scan struct {
	Host string
	Port int
	Keys []HostKey
}

// Fingerprints renders the keys for display, one per line.
func (s Scan) Fingerprints() string {
	var b strings.Builder

	for _, key := range s.Keys {
		fmt.Fprintf(&b, "%s %s\n", key.Type, key.Fingerprint)
	}

	return strings.TrimRight(b.String(), "\n")
}

// ScanHost asks the server which host keys it offers.
//
// The keys are not trusted yet. They are shown to the user first, because
// accepting a host key without looking is the one step of an ssh setup that
// cannot be undone by editing a file afterwards.
func ScanHost(ctx context.Context, host string, port int) (Scan, error) {
	args := []string{"-T", "5"}
	if port != 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}

	args = append(args, host)

	out, err := runLocal(ctx, nil, "ssh-keyscan", args...)
	if err != nil {
		return Scan{}, err
	}

	scan := Scan{Host: host, Port: port}

	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)

		// ssh-keyscan writes its progress as comments on stdout.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, err := describeKey(ctx, line)
		if err != nil {
			return Scan{}, err
		}

		scan.Keys = append(scan.Keys, key)
	}

	if len(scan.Keys) == 0 {
		return Scan{}, fmt.Errorf("%s sunucusundan host key alınamadı, adres ve port doğru mu", host)
	}

	return scan, nil
}

// describeKey turns a known_hosts line into something a person can compare
// against what the server administrator told them.
func describeKey(ctx context.Context, line string) (HostKey, error) {
	out, err := runLocal(ctx, strings.NewReader(line+"\n"), "ssh-keygen", "-l", "-f", "-")
	if err != nil {
		return HostKey{}, err
	}

	key := HostKey{Line: line}

	// The output is "<bits> <fingerprint> <comment> (<TYPE>)".
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) >= 2 {
		key.Fingerprint = fields[1]
	}

	if len(fields) >= 1 {
		last := fields[len(fields)-1]
		key.Type = strings.Trim(last, "()")
	}

	if key.Fingerprint == "" {
		return HostKey{}, fmt.Errorf("host key parmak izi okunamadı")
	}

	return key, nil
}

// KnownHostsState says what the known_hosts file already holds for a host.
type KnownHostsState int

const (
	// HostUnknown means there is no entry yet, so the scanned keys can be
	// added once the user has seen them.
	HostUnknown KnownHostsState = iota

	// HostMatches means the entry is already there and agrees with the scan.
	HostMatches

	// HostChanged means an entry exists and offers a different key. Either the
	// server was rebuilt or the connection is not reaching the same machine,
	// and neither is something to write over.
	HostChanged
)

// CheckKnownHosts compares the scanned keys against the known_hosts file.
func CheckKnownHosts(path string, scan Scan) (KnownHostsState, error) {
	data, err := readIfExists(path)
	if err != nil {
		return HostUnknown, err
	}

	if len(data) == 0 {
		return HostUnknown, nil
	}

	pattern := hostPattern(scan.Host, scan.Port)

	scanned := make(map[string]string, len(scan.Keys))
	for _, key := range scan.Keys {
		if algo, material, ok := keyMaterial(key.Line); ok {
			scanned[algo] = material
		}
	}

	state := HostUnknown

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 || !matchesHost(fields[0], pattern) {
			continue
		}

		algo, material := fields[1], fields[2]

		want, offered := scanned[algo]
		if !offered {
			// The file holds an algorithm the server no longer offers, which
			// says nothing on its own.
			continue
		}

		if want != material {
			return HostChanged, nil
		}

		state = HostMatches
	}

	return state, nil
}

// AppendKnownHosts adds the scanned keys to the file.
func AppendKnownHosts(path string, scan Scan) error {
	if err := ensureDir(dirOf(path)); err != nil {
		return err
	}

	if err := backup(path); err != nil {
		return err
	}

	existing, err := readIfExists(path)
	if err != nil {
		return err
	}

	var b strings.Builder

	b.Write(existing)

	// Appending to a file whose last line has no newline would join the two.
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}

	for _, key := range scan.Keys {
		b.WriteString(key.Line)
		b.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), FileMode); err != nil {
		return fmt.Errorf("known_hosts yazılamadı (%s): %w", path, err)
	}

	return nil
}

// hostPattern renders the host the way known_hosts stores it. A non-default
// port is bracketed, which is why the plain host name would not match.
func hostPattern(host string, port int) string {
	if port == 0 || port == 22 {
		return host
	}

	return fmt.Sprintf("[%s]:%d", host, port)
}

// matchesHost reports whether a known_hosts host field covers the pattern. The
// field may list several hosts separated by commas.
func matchesHost(field, pattern string) bool {
	for candidate := range strings.SplitSeq(field, ",") {
		if candidate == pattern {
			return true
		}
	}

	return false
}

// keyMaterial splits a known_hosts line into its algorithm and key.
func keyMaterial(line string) (algo, material string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", "", false
	}

	return fields[1], fields[2], true
}
