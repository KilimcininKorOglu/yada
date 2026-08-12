package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
	"github.com/kerem/unbound-dns/internal/unbound"
	"github.com/spf13/cobra"
)

func newAddCommand() *cobra.Command {
	var ttl uint32

	cmd := &cobra.Command{
		Use:   "add <ad> <tip> <değer>",
		Short: "Tüm sunuculara bir DNS kaydı ekler",
		Long: "Kaydı her sunucunun kayıt dosyasına ekler, ana config'i\n" +
			"unbound-checkconf ile doğrular ve doğrulama başarısız olursa\n" +
			"dosyayı yedekten geri yükler.\n\n" +
			"Üst zone tanımlı değilse transparent olarak eklenir.",
		Example: "  unbound-dns add mail.google.com A 10.10.10.10\n" +
			"  unbound-dns add web.local CNAME db.local. --ttl 3600\n" +
			"  unbound-dns add example.com MX \"10 mail.example.com.\"\n" +
			"  unbound-dns add 10.10.10.10.in-addr.arpa PTR mail.google.com.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			var ttlPtr *uint32
			if cmd.Flags().Changed("ttl") {
				ttlPtr = &ttl
			}

			return runAdd(cmd.Context(), args[0], args[1], args[2], ttlPtr)
		},
	}

	cmd.Flags().Uint32Var(&ttl, "ttl", 0, "kayıt için TTL saniyesi (verilmezse Unbound varsayılanı kullanılır)")

	return cmd
}

func runAdd(ctx context.Context, name, typeName, value string, ttl *uint32) error {
	recType, err := records.ParseType(typeName)
	if err != nil {
		return err
	}

	rec, err := records.New(name, recType, value, ttl)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	runner := transport.NewSSHRunner(cfg.SSH)

	opts := unbound.WriteOptions{
		Backup: cfg.Behaviour.BackupBeforeWrite,
		DryRun: flags.dryRun,
	}

	// Each server gets its own parsed file, so a record already present on one
	// server does not block the others from receiving it.
	results := unbound.Apply(ctx, runner, cfg, opts, func(f *records.File) error {
		return f.Add(rec)
	})

	if err := reportWriteResults("Eklenecek kayıt", rec, results, opts.DryRun); err != nil {
		return err
	}

	return refreshChanged(ctx, runner, cfg, results)
}

// refreshChanged makes the write take effect on the servers it actually
// changed. A server that was skipped or failed has nothing new to pick up.
//
// The write results carry the records that moved, so the refresh can push them
// straight into the running daemon instead of making it re-read its config.
func refreshChanged(ctx context.Context, runner transport.Runner, cfg config.Config, results []unbound.WriteResult) error {
	if flags.dryRun || len(unbound.ChangedServers(results)) == 0 {
		return nil
	}

	if flags.noReload {
		fmt.Println("\nYenileme atlandı (--no-reload). Devreye almak için: unbound-dns reload")
		return nil
	}

	fmt.Println("\nServisler yenileniyor...")

	return reportReloadResults(unbound.RefreshWrites(ctx, runner, cfg, results), cfg.Behaviour.ReloadStrategy)
}

// reportWriteResults prints what happened per server and turns failures into
// an exit code.
func reportWriteResults(title string, rec records.Record, results []unbound.WriteResult, dryRun bool) error {
	fmt.Printf("%s: %s\n", title, rec.String())

	if dryRun {
		fmt.Println("(dry-run: hiçbir değişiklik yapılmadı)")
	}

	fmt.Println()

	var failed int

	for _, res := range results {
		label := res.Server.Label()

		switch {
		case res.Err != nil && isAlreadyExists(res.Err):
			// Not a failure: the desired state is already in place.
			fmt.Printf("[%s] atlandı: %s\n", label, res.Err)

		case res.Err != nil:
			failed++
			fmt.Printf("[%s] BAŞARISIZ: %s\n", label, res.Err)

			if res.RolledBack {
				fmt.Printf("[%s] dosya yedekten geri yüklendi, sunucuda değişiklik kalmadı\n", label)
			}
			if res.CheckOutput != "" {
				printIndented(res.CheckOutput)
			}

		case res.Diff.Empty():
			fmt.Printf("[%s] değişiklik yok\n", label)

		case dryRun:
			fmt.Printf("[%s] yapılacak değişiklik:\n", label)
			fmt.Print(indentBlock(res.Diff.String()))

		default:
			fmt.Printf("[%s] eklendi\n", label)
			fmt.Print(indentBlock(res.Diff.String()))
		}
	}

	if failed > 0 {
		return &exitCodeError{
			code: exitCodeFor(failed, len(results)),
			msg:  fmt.Sprintf("%d sunucuda işlem başarısız oldu", failed),
		}
	}

	return nil
}

// isAlreadyExists reports whether the failure is only that the record is
// already present, which is an acceptable outcome rather than an error.
func isAlreadyExists(err error) bool {
	var exists *records.ErrExists
	return errors.As(err, &exists)
}

func indentBlock(text string) string {
	var b strings.Builder

	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(&b, "    %s\n", line)
	}

	return b.String()
}
