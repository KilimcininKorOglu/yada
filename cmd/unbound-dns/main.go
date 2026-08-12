// Command unbound-dns manages local DNS records on Unbound servers over SSH.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kerem/unbound-dns/internal/config"
)

// Exit codes are part of the CLI contract, see plan.md section 9.
const (
	exitOK          = 0
	exitConfigError = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ayar okunamadı:")
		fmt.Fprintln(os.Stderr, err)

		// The search list only helps when nothing was found; once a file is
		// located the error already names it.
		if errors.Is(err, config.ErrNotFound) {
			fmt.Fprintln(os.Stderr, "\nAranan konumlar:")
			for _, dir := range config.SearchDirs() {
				fmt.Fprintf(os.Stderr, "  %s\n", filepath.Join(dir, config.FileName))
			}
			fmt.Fprintf(os.Stderr, "\nÖrnek dosya: %s\n", config.FileName+".example")
		}

		return exitConfigError
	}

	fmt.Printf("Ayar dosyası: %s\n", cfg.SourcePath)
	fmt.Printf("Yenileme stratejisi: %s\n\n", cfg.Behaviour.ReloadStrategy)
	fmt.Printf("Tanımlı sunucular (%d):\n", len(cfg.Servers))

	for _, srv := range cfg.Servers {
		fmt.Printf("  %s  %s@%s:%d  kayıtlar=%s  config=%s  sudo=%t\n",
			srv.Label(), srv.User, srv.Host, srv.Port, srv.RecordsFile, srv.MainConfig, srv.UseSudo())
	}

	return exitOK
}
