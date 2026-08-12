package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
	"github.com/kerem/unbound-dns/internal/unbound"
	"github.com/spf13/cobra"
)

func newDeleteCommand() *cobra.Command {
	var (
		typeName string
		value    string
		prune    bool
	)

	cmd := &cobra.Command{
		Use:     "delete <ad>",
		Aliases: []string{"remove", "rm"},
		Short:   "Tüm sunuculardan bir kaydı siler",
		Long: "Adı eşleşen kayıtları siler. --type ve --value ile daraltılabilir.\n" +
			"Silme öncesi eşleşen kayıtlar listelenir ve onay istenir.",
		Example: "  unbound-dns delete eski.google.com\n" +
			"  unbound-dns delete mail.google.com --type A\n" +
			"  unbound-dns delete mail.google.com --type A --value 10.10.10.10",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return runDelete(cmd.Context(), args[0], typeName, value, prune)
		},
	}

	cmd.Flags().StringVar(&typeName, "type", "", "yalnızca bu tipteki kayıtları sil")
	cmd.Flags().StringVar(&value, "value", "", "yalnızca bu değere sahip kaydı sil")
	cmd.Flags().BoolVar(&prune, "prune-zones", true, "kaydı kalmayan, araç tarafından eklenmiş zone satırlarını da temizle")

	return cmd
}

func runDelete(ctx context.Context, name, typeName, value string, prune bool) error {
	var recType records.Type

	if typeName != "" {
		parsed, err := records.ParseType(typeName)
		if err != nil {
			return err
		}
		recType = parsed
	}

	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	runner := transport.NewSSHRunner(cfg.SSH)

	// Show what will go before asking, so the confirmation is informed rather
	// than a blind yes.
	matched, err := previewDeletion(ctx, runner, cfg, name, recType, value)
	if err != nil {
		return err
	}

	if matched == 0 {
		fmt.Println("Eşleşen kayıt yok, silinecek bir şey bulunamadı.")
		return nil
	}

	if cfg.Behaviour.ConfirmDestructive && !flags.dryRun {
		ok, err := confirm(fmt.Sprintf("Bu %d kayıt silinsin mi?", matched))
		if err != nil {
			return err
		}

		if !ok {
			fmt.Println("İşlem iptal edildi.")
			return nil
		}
	}

	opts := unbound.WriteOptions{
		Backup: cfg.Behaviour.BackupBeforeWrite,
		DryRun: flags.dryRun,
	}

	results := unbound.Apply(ctx, runner, cfg, opts, func(f *records.File) error {
		if f.Delete(name, recType, value) == 0 {
			return nil
		}

		if prune {
			f.PruneUnusedZones()
		}

		return nil
	})

	if err := reportDeleteResults(results, opts.DryRun); err != nil {
		return err
	}

	return refreshChanged(ctx, runner, cfg, results)
}

// previewDeletion lists the records that match, per server, and returns how
// many were found in total.
func previewDeletion(
	ctx context.Context,
	runner transport.Runner,
	cfg config.Config,
	name string,
	recType records.Type,
	value string,
) (int, error) {
	results := unbound.ReadAll(ctx, runner, cfg)

	total := 0
	failed := 0

	for _, res := range results {
		if res.Err != nil {
			failed++
			fmt.Printf("[%s] okunamadı: %s\n", res.Server.Label(), res.Err)

			continue
		}

		matches := filterMatches(res.File.Find(name, recType), value)
		if len(matches) == 0 {
			continue
		}

		fmt.Printf("[%s] silinecek:\n", res.Server.Label())

		for _, rec := range matches {
			fmt.Printf("    %s\n", rec.String())
			total++
		}
	}

	if failed == len(results) && failed > 0 {
		return 0, &exitCodeError{code: exitError, msg: "hiçbir sunucu okunamadı"}
	}

	return total, nil
}

func filterMatches(found []records.Record, value string) []records.Record {
	if value == "" {
		return found
	}

	var out []records.Record

	for _, rec := range found {
		if rec.Value == value {
			out = append(out, rec)
		}
	}

	return out
}

func reportDeleteResults(results []unbound.WriteResult, dryRun bool) error {
	if dryRun {
		fmt.Println("\n(dry-run: hiçbir değişiklik yapılmadı)")
	}

	fmt.Println()

	var failed int

	for _, res := range results {
		label := res.Server.Label()

		switch {
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
			fmt.Printf("[%s] eşleşen kayıt yok\n", label)

		case dryRun:
			fmt.Printf("[%s] yapılacak değişiklik:\n", label)
			fmt.Print(indentBlock(res.Diff.String()))

		default:
			fmt.Printf("[%s] silindi\n", label)
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

func newUpdateCommand() *cobra.Command {
	var (
		typeName string
		value    string
		ttl      uint32
	)

	cmd := &cobra.Command{
		Use:   "update <ad>",
		Short: "Var olan bir kaydın değerini değiştirir",
		Long: "Kaydı yerinde günceller, dosyadaki sırasını korur.\n" +
			"Kayıt yoksa hata verir; yeni kayıt için add kullanın.",
		Example: "  unbound-dns update mail.google.com --type A --value 10.20.30.40\n" +
			"  unbound-dns update mail.google.com --type A --value 10.20.30.40 --ttl 300",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			if value == "" {
				return fmt.Errorf("--value zorunlu")
			}
			if typeName == "" {
				return fmt.Errorf("--type zorunlu (A, AAAA, CNAME, TXT, MX, PTR)")
			}

			var ttlPtr *uint32
			if cmd.Flags().Changed("ttl") {
				ttlPtr = &ttl
			}

			return runUpdate(cmd.Context(), args[0], typeName, value, ttlPtr)
		},
	}

	cmd.Flags().StringVar(&typeName, "type", "", "güncellenecek kaydın tipi")
	cmd.Flags().StringVar(&value, "value", "", "yeni değer")
	cmd.Flags().Uint32Var(&ttl, "ttl", 0, "yeni TTL saniyesi")

	return cmd
}

func runUpdate(ctx context.Context, name, typeName, value string, ttl *uint32) error {
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

	results := unbound.Apply(ctx, runner, cfg, opts, func(f *records.File) error {
		return f.Update(rec)
	})

	if err := reportUpdateResults(rec, results, opts.DryRun); err != nil {
		return err
	}

	return refreshChanged(ctx, runner, cfg, results)
}

func reportUpdateResults(rec records.Record, results []unbound.WriteResult, dryRun bool) error {
	fmt.Printf("Yeni değer: %s\n", rec.String())

	if dryRun {
		fmt.Println("(dry-run: hiçbir değişiklik yapılmadı)")
	}

	fmt.Println()

	var failed int

	for _, res := range results {
		label := res.Server.Label()

		switch {
		case res.Err != nil && strings.Contains(res.Err.Error(), "bulunamadı"):
			// The record is missing on this server, which update cannot fix;
			// say so plainly and point at the command that can.
			failed++
			fmt.Printf("[%s] BAŞARISIZ: %s (eklemek için: unbound-dns add)\n", label, res.Err)

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
			fmt.Printf("[%s] değer zaten aynı\n", label)

		case dryRun:
			fmt.Printf("[%s] yapılacak değişiklik:\n", label)
			fmt.Print(indentBlock(res.Diff.String()))

		default:
			fmt.Printf("[%s] güncellendi\n", label)
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
