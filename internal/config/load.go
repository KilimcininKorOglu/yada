package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned when no configuration file exists in any search
// location.
var ErrNotFound = errors.New("ayar dosyası bulunamadı")

// ExecutableDir returns the directory holding the running binary, with any
// symlink resolved. Without resolving, a binary started through a symlink
// would look for its configuration next to the link rather than next to
// itself.
func ExecutableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("çalışan uygulamanın yolu bulunamadı: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// A broken symlink should not make the whole lookup fail; fall back to
		// the unresolved path.
		resolved = exe
	}

	return filepath.Dir(resolved), nil
}

// SearchDirs lists the directories probed for the configuration file, in
// priority order: next to the executable first, then the home directory.
func SearchDirs() []string {
	var dirs []string

	if dir, err := ExecutableDir(); err == nil {
		dirs = append(dirs, dir)
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, home)
	}

	return dirs
}

// FindIn returns the first existing configuration file among the given
// directories.
func FindIn(dirs []string) (string, error) {
	for _, dir := range dirs {
		candidate := filepath.Join(dir, FileName)

		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", ErrNotFound
}

// Find locates the configuration file using the standard search order.
func Find() (string, error) {
	return FindIn(SearchDirs())
}

// Default returns a configuration with every optional field populated. Loading
// decodes on top of this value, so a field absent from the file keeps its
// default instead of falling back to the zero value.
func Default() Config {
	sudo := true

	return Config{
		Defaults: Defaults{
			Port:        22,
			RecordsFile: "/etc/unbound/local_records.conf",
			MainConfig:  "/etc/unbound/unbound.conf",
			Sudo:        &sudo,
		},
		SSH: SSH{
			Binary:         "ssh",
			ConnectTimeout: Duration(10 * time.Second),
			Options:        []string{"BatchMode=yes"},
		},
		Behaviour: Behaviour{
			Parallel:           true,
			MaxParallel:        4,
			BackupBeforeWrite:  true,
			ReloadStrategy:     ReloadAuto,
			ConfirmDestructive: true,
		},
		Log: Log{
			Level: LogInfo,
		},
	}
}

// Load reads, decodes and validates the configuration at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("ayar dosyası okunamadı (%s): %w", path, err)
	}

	cfg, err := Decode(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}

	cfg.SourcePath = path
	return cfg, nil
}

// Decode parses configuration bytes, applies defaults and validates the
// result. Unknown fields are rejected so a misspelled key surfaces instead of
// being silently ignored.
func Decode(data []byte) (Config, error) {
	cfg := Default()

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("ayar dosyası çözümlenemedi: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// LoadDefault finds and loads the configuration from the standard locations.
func LoadDefault() (Config, error) {
	path, err := Find()
	if err != nil {
		return Config{}, err
	}

	return Load(path)
}

// applyDefaults copies the defaults block into every server that omitted a
// field.
func (c *Config) applyDefaults() {
	for i := range c.Servers {
		srv := &c.Servers[i]

		if srv.User == "" {
			srv.User = c.Defaults.User
		}
		if srv.Port == 0 {
			srv.Port = c.Defaults.Port
		}
		if srv.RecordsFile == "" {
			srv.RecordsFile = c.Defaults.RecordsFile
		}
		if srv.MainConfig == "" {
			srv.MainConfig = c.Defaults.MainConfig
		}
		if srv.Sudo == nil {
			srv.Sudo = c.Defaults.Sudo
		}
	}
}

// UseSudo reports whether remote commands for this server run under sudo.
func (s Server) UseSudo() bool {
	return s.Sudo != nil && *s.Sudo
}
