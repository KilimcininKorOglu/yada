// Package sshsetup prepares the local OpenSSH side of a server: the private
// key file, the known_hosts entry and the Host block in the ssh configuration.
//
// Everything here writes files the user also owns and edits by hand, so each
// write is narrow: only the block this tool wrote is replaced, the rest of the
// file is carried over untouched, and a backup is taken first.
package sshsetup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// DirMode is what OpenSSH expects of ~/.ssh. A wider directory makes ssh
	// refuse the keys inside it.
	DirMode fs.FileMode = 0o700

	// KeyMode is what OpenSSH expects of a private key.
	KeyMode fs.FileMode = 0o600

	// FileMode covers the files that are not secret but still private.
	FileMode fs.FileMode = 0o644

	// BackupSuffix is appended before a file the user may have edited is
	// rewritten.
	BackupSuffix = ".bak"

	// KeyPrefix marks the key files this tool wrote, which is how a key can be
	// reused rather than copied a second time.
	KeyPrefix = "yada_"
)

// Paths holds the local OpenSSH files a setup touches.
type Paths struct {
	Dir        string
	Config     string
	KnownHosts string
}

// Locate resolves the files to write.
//
// configFile is the ssh.config_file setting. When it is set the tool passes it
// to ssh with -F, so that is the only configuration ssh reads and the Host
// block has to go there rather than into ~/.ssh/config.
func Locate(configFile string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("kullanıcı ana dizini bulunamadı: %w", err)
	}

	dir := filepath.Join(home, ".ssh")

	paths := Paths{
		Dir:        dir,
		Config:     filepath.Join(dir, "config"),
		KnownHosts: filepath.Join(dir, "known_hosts"),
	}

	if configFile != "" {
		paths.Config = configFile
	}

	return paths, nil
}

// ensureDir creates a directory with the mode OpenSSH requires.
func ensureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}

	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("dizin oluşturulamadı (%s): %w", dir, err)
	}

	return nil
}

// backup copies a file before it is rewritten. A missing file is not an error:
// there is nothing to lose yet.
func backup(path string) error {
	// The path is one this package resolved or the operator configured, and
	// the copy stays beside it.
	// #nosec G304
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("%s okunamadı: %w", path, err)
	}

	mode := FileMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	// The backup is written beside the file it copies, and that file is either
	// resolved from the home directory by Locate or named by the operator as
	// ssh.config_file. It is about to be rewritten anyway, so the copy reaches
	// nothing the caller could not already reach.
	// #nosec G703
	if err := os.WriteFile(path+BackupSuffix, data, mode); err != nil {
		return fmt.Errorf("%s yedeklenemedi: %w", path, err)
	}

	return nil
}

// readIfExists returns the file contents, or nothing when it does not exist.
func readIfExists(path string) ([]byte, error) {
	// Same as backup: the path comes from Locate or from the configuration.
	// #nosec G304
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("%s okunamadı: %w", path, err)
	}

	return data, nil
}
