package sshsetup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// unsafeNameChars is everything that does not belong in a file name. The
// server name reaches this package straight from a form field, and the result
// becomes a path.
var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// KeyResult reports where the private key ended up.
type KeyResult struct {
	Path string

	// Reused is true when an identical key was already on disk and no second
	// copy was written. Servers are allowed to share a key, and copying it per
	// server would leave several files to rotate instead of one.
	Reused bool
}

// WriteKey stores a private key and returns the file ssh should use.
//
// The key is validated before it is kept, because a truncated paste produces a
// file ssh rejects with a message that says nothing about where it came from.
func WriteKey(ctx context.Context, dir, name string, key []byte) (KeyResult, error) {
	key = normaliseKey(key)
	if len(key) == 0 {
		return KeyResult{}, fmt.Errorf("private key boş")
	}

	if err := ensureDir(dir); err != nil {
		return KeyResult{}, err
	}

	if existing, err := findIdenticalKey(dir, key); err != nil {
		return KeyResult{}, err
	} else if existing != "" {
		return KeyResult{Path: existing, Reused: true}, nil
	}

	path := filepath.Join(dir, KeyPrefix+safeName(name))

	if err := writeValidatedKey(ctx, path, key); err != nil {
		return KeyResult{}, err
	}

	return KeyResult{Path: path}, nil
}

// writeValidatedKey writes the key to a temporary neighbour, checks it, and
// only then moves it into place. Writing straight to the target would leave a
// rejected key behind under the name ssh is about to be told to use.
func writeValidatedKey(ctx context.Context, path string, key []byte) error {
	temp := path + ".tmp"

	if err := os.WriteFile(temp, key, KeyMode); err != nil {
		return fmt.Errorf("anahtar yazılamadı (%s): %w", temp, err)
	}

	if _, err := runLocal(ctx, nil, "ssh-keygen", "-y", "-f", temp); err != nil {
		// The key itself must not reach the message, and neither must the
		// tool's own output, which echoes the file it could not read.
		_ = os.Remove(temp)

		return fmt.Errorf("private key okunamadı, biçimi bozuk veya parola korumalı olabilir")
	}

	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)

		return fmt.Errorf("anahtar yerine konamadı (%s): %w", path, err)
	}

	// Rename keeps the temporary file's mode, but an existing target could
	// have had a wider one.
	if err := os.Chmod(path, KeyMode); err != nil {
		return fmt.Errorf("anahtar izinleri ayarlanamadı (%s): %w", path, err)
	}

	return nil
}

// findIdenticalKey returns the path of a key this tool already wrote with the
// same content.
func findIdenticalKey(dir string, key []byte) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("%s okunamadı: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), KeyPrefix) {
			continue
		}

		// Only files this tool wrote are compared, so a key the user manages
		// themselves is never adopted silently.
		if strings.HasSuffix(entry.Name(), ".pub") || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		data, err := readIfExists(path)
		if err != nil {
			return "", err
		}

		if bytes.Equal(normaliseKey(data), key) {
			return path, nil
		}
	}

	return "", nil
}

// normaliseKey trims the whitespace a paste picks up and restores the trailing
// newline OpenSSH requires.
func normaliseKey(key []byte) []byte {
	trimmed := bytes.TrimSpace(key)
	if len(trimmed) == 0 {
		return nil
	}

	// A pasted key can arrive with Windows line endings, which the OpenSSH
	// format does not accept.
	trimmed = bytes.ReplaceAll(trimmed, []byte("\r\n"), []byte("\n"))
	trimmed = bytes.ReplaceAll(trimmed, []byte("\r"), []byte("\n"))

	return append(trimmed, '\n')
}

// safeName turns a server name into a file name. An empty or fully stripped
// name still has to produce something usable.
func safeName(name string) string {
	cleaned := unsafeNameChars.ReplaceAllString(strings.TrimSpace(name), "_")
	cleaned = strings.Trim(cleaned, "._-")

	if cleaned == "" {
		return "key"
	}

	return cleaned
}
