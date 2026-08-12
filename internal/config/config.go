// Package config loads and validates the yada.conf file.
package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/KilimcininKorOglu/yada"
)

// FileName is the fixed name looked up next to the executable and in the home
// directory.
const FileName = "yada.conf"

// Example is the commented starter configuration, embedded at the repository
// root so the file stays visible there rather than buried in this package.
var Example = yada.ConfExample

// ReloadStrategy selects which refresh tiers are attempted after a write.
type ReloadStrategy string

const (
	// ReloadAuto tries the local_data push, then reload_keep_cache, then
	// systemctl reload, then restart.
	ReloadAuto ReloadStrategy = "auto"
	// ReloadLocalData only pushes the changed records with unbound-control
	// local_data. It needs no config re-read at all, but it can only run after
	// a write this tool performed, because the change set has to be known.
	ReloadLocalData ReloadStrategy = "local_data"
	// ReloadControl only runs unbound-control reload_keep_cache.
	ReloadControl ReloadStrategy = "control"
	// ReloadSignal only runs systemctl reload, which delivers SIGHUP.
	ReloadSignal ReloadStrategy = "signal"
	// ReloadRestart only runs systemctl restart.
	ReloadRestart ReloadStrategy = "restart"
)

// LogLevel controls verbosity.
type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

// Duration wraps time.Duration so YAML can carry values like "10s".
type Duration time.Duration

// UnmarshalYAML decodes a duration string such as "10s" or "1m30s".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("süre bir metin olmalı (örnek: 10s): %w", err)
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("geçersiz süre %q (örnek: 10s, 1m30s)", raw)
	}

	*d = Duration(parsed)
	return nil
}

// MarshalYAML writes the duration back as a string.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// Std converts back to the standard library type.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// Config is the whole configuration file.
type Config struct {
	Servers   []Server  `yaml:"servers"`
	Defaults  Defaults  `yaml:"defaults"`
	SSH       SSH       `yaml:"ssh"`
	Behaviour Behaviour `yaml:"behaviour"`
	Log       Log       `yaml:"log"`

	// SourcePath records which file this config was read from. It is not part
	// of the file itself.
	SourcePath string `yaml:"-"`
}

// Server is a single Unbound host reachable over SSH.
type Server struct {
	Name        string `yaml:"name,omitempty"`
	Host        string `yaml:"host"`
	User        string `yaml:"user,omitempty"`
	Port        int    `yaml:"port,omitempty"`
	RecordsFile string `yaml:"records_file,omitempty"`
	MainConfig  string `yaml:"main_config,omitempty"`

	// Sudo is a pointer so an explicit false is distinguishable from an
	// omitted field, which must inherit the default instead.
	Sudo *bool `yaml:"sudo,omitempty"`
}

// Label returns the display name, falling back to the host.
func (s Server) Label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Host
}

// Defaults fills in fields a server omits.
type Defaults struct {
	User        string `yaml:"user,omitempty"`
	Port        int    `yaml:"port,omitempty"`
	RecordsFile string `yaml:"records_file,omitempty"`
	MainConfig  string `yaml:"main_config,omitempty"`
	Sudo        *bool  `yaml:"sudo,omitempty"`
}

// SSH controls how the system ssh binary is invoked.
type SSH struct {
	Binary         string   `yaml:"binary,omitempty"`
	ConnectTimeout Duration `yaml:"connect_timeout,omitempty"`
	Options        []string `yaml:"options,omitempty"`
	ConfigFile     string   `yaml:"config_file,omitempty"`
}

// Behaviour holds run-time switches that are not tied to a single server.
type Behaviour struct {
	Parallel           bool           `yaml:"parallel"`
	MaxParallel        int            `yaml:"max_parallel,omitempty"`
	BackupBeforeWrite  bool           `yaml:"backup_before_write"`
	ReloadStrategy     ReloadStrategy `yaml:"reload_strategy,omitempty"`
	ConfirmDestructive bool           `yaml:"confirm_destructive"`
}

// Log controls where diagnostics go.
type Log struct {
	Level LogLevel `yaml:"level,omitempty"`
	File  string   `yaml:"file,omitempty"`
}
