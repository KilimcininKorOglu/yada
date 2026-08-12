package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/spf13/cobra"
)

// version is set at build time through -ldflags.
var version = "dev"

// Exit codes are part of the CLI contract, see plan.md section 9.
const (
	exitOK           = 0
	exitError        = 1
	exitConfigError  = 2
	exitPartialError = 5
)

type globalFlags struct {
	configPath string
	servers    []string
	jsonOutput bool
	dryRun     bool
}

var flags globalFlags

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "unbound-dns",
		Short: "Unbound DNS sunucularındaki yerel kayıtları yönetir",
		Long: "unbound-dns, bir veya birden fazla Unbound sunucusundaki yerel DNS\n" +
			"kayıtlarını SSH üzerinden yönetir.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.PersistentFlags().StringVar(&flags.configPath, "config", "",
		"ayar dosyası yolu (varsayılan: uygulama yanı, sonra kullanıcı dizini)")
	root.PersistentFlags().StringSliceVar(&flags.servers, "server", nil,
		"yalnızca bu sunuculara uygula (ad veya host, tekrarlanabilir)")
	root.PersistentFlags().BoolVar(&flags.jsonOutput, "json", false,
		"çıktıyı JSON olarak ver")
	root.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false,
		"değişiklik yapmadan yalnızca ne olacağını göster")

	root.AddCommand(newCheckCommand())
	root.AddCommand(newConfigCommand())
	root.AddCommand(newListCommand())
	root.AddCommand(newAddCommand())

	return root
}

// loadConfig resolves the configuration and applies the --server filter.
func loadConfig() (config.Config, error) {
	var (
		cfg config.Config
		err error
	)

	if flags.configPath != "" {
		cfg, err = config.Load(flags.configPath)
	} else {
		cfg, err = config.LoadDefault()
	}

	if err != nil {
		return config.Config{}, err
	}

	if len(flags.servers) > 0 {
		cfg, err = filterServers(cfg, flags.servers)
		if err != nil {
			return config.Config{}, err
		}
	}

	return cfg, nil
}

// filterServers narrows the configuration to the named servers, matching
// either the display name or the host.
func filterServers(cfg config.Config, wanted []string) (config.Config, error) {
	var (
		kept    []config.Server
		unknown []string
	)

	for _, want := range wanted {
		found := false

		for _, srv := range cfg.Servers {
			if srv.Name == want || srv.Host == want {
				kept = append(kept, srv)
				found = true
				break
			}
		}

		if !found {
			unknown = append(unknown, want)
		}
	}

	if len(unknown) > 0 {
		available := make([]string, 0, len(cfg.Servers))
		for _, srv := range cfg.Servers {
			available = append(available, srv.Label())
		}

		return config.Config{}, fmt.Errorf("bilinmeyen sunucu: %v (tanımlı olanlar: %v)", unknown, available)
	}

	cfg.Servers = kept
	return cfg, nil
}

// reportConfigError prints a configuration failure, adding the search list
// only when nothing was found. Once a file is located the error names it.
func reportConfigError(err error) int {
	fmt.Fprintln(os.Stderr, "Ayar okunamadı:")
	fmt.Fprintln(os.Stderr, err)

	if errors.Is(err, config.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "\nAranan konumlar:")
		for _, dir := range config.SearchDirs() {
			fmt.Fprintf(os.Stderr, "  %s\n", filepath.Join(dir, config.FileName))
		}
		fmt.Fprintln(os.Stderr, "\nÖrnek dosyayı kopyalamak için: unbound-dns config init")
	}

	return exitConfigError
}
