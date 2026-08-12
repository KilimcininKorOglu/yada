package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Ayar dosyasını oluşturur ve gösterir",
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(newConfigInitCommand())
	cmd.AddCommand(newConfigShowCommand())

	return cmd
}

func newConfigInitCommand() *cobra.Command {
	var (
		target string
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Örnek ayar dosyasını oluşturur",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			path, err := resolveInitTarget(target)
			if err != nil {
				return err
			}

			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s zaten var, üzerine yazmak için --force kullanın", path)
			}

			if err := os.WriteFile(path, config.Example, 0o600); err != nil {
				return fmt.Errorf("ayar dosyası yazılamadı: %w", err)
			}

			fmt.Printf("Ayar dosyası oluşturuldu: %s\n", path)
			fmt.Println("Sunucu adreslerini ve kullanıcı adını düzenleyin, sonra: yada check")

			return nil
		},
	}

	cmd.Flags().StringVar(&target, "path", "",
		"hedef yol (varsayılan: uygulamanın bulunduğu dizin)")
	cmd.Flags().BoolVar(&force, "force", false, "var olan dosyanın üzerine yaz")

	return cmd
}

// resolveInitTarget picks where the example is written. The executable
// directory is the first search location, so writing there makes the file
// findable straight away.
func resolveInitTarget(target string) (string, error) {
	if target != "" {
		info, err := os.Stat(target)
		if err == nil && info.IsDir() {
			return filepath.Join(target, config.FileName), nil
		}

		return target, nil
	}

	dir, err := config.ExecutableDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, config.FileName), nil
}

func newConfigShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Etkin ayarı ve hangi dosyadan okunduğunu gösterir",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			cfg, err := loadConfig()
			if err != nil {
				return &exitCodeError{code: reportConfigError(err)}
			}

			fmt.Printf("Ayar dosyası: %s\n", cfg.SourcePath)
			fmt.Printf("Yenileme stratejisi: %s\n", cfg.Behaviour.ReloadStrategy)
			fmt.Printf("Eşzamanlılık: %t (en fazla %d)\n", cfg.Behaviour.Parallel, cfg.Behaviour.MaxParallel)
			fmt.Printf("Yazmadan önce yedek: %t\n", cfg.Behaviour.BackupBeforeWrite)
			fmt.Printf("ssh: %s (zaman aşımı %s)\n\n", cfg.SSH.Binary, cfg.SSH.ConnectTimeout.Std())

			fmt.Printf("Sunucular (%d):\n", len(cfg.Servers))
			for _, srv := range cfg.Servers {
				port := "ssh varsayılanı"
				if srv.Port > 0 {
					port = fmt.Sprintf("%d", srv.Port)
				}

				fmt.Printf("  %s\n", srv.Label())
				fmt.Printf("    adres      : %s@%s (port: %s)\n", srv.User, srv.Host, port)
				fmt.Printf("    kayıtlar   : %s\n", srv.RecordsFile)
				fmt.Printf("    ana config : %s\n", srv.MainConfig)
				fmt.Printf("    sudo       : %t\n", srv.UseSudo())
			}

			return nil
		},
	}
}

// exitCodeError carries a specific process exit code out of a cobra command.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	return e.msg
}

// codeOf extracts the exit code an error asks for, defaulting to a generic
// failure.
func codeOf(err error) int {
	if e, ok := errors.AsType[*exitCodeError](err); ok {
		return e.code
	}
	return exitError
}
