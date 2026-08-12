package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/kerem/unbound-dns/internal/bulk"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
	"github.com/kerem/unbound-dns/internal/unbound"
	"github.com/spf13/cobra"
)

func newImportCommand() *cobra.Command {
	var replace bool

	cmd := &cobra.Command{
		Use:   "import <dosya.csv>",
		Short: "CSV dosyasından toplu kayıt ekler",
		Long: "Başlık satırı zorunludur: name, type, value ve isteğe bağlı ttl.\n" +
			"Sütun sırası serbesttir, adla eşleştirilir.\n\n" +
			"Hatalı satırlar satır numarasıyla bildirilir ve atlanır; geçerli\n" +
			"satırlar yine de uygulanır.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return runImport(cmd.Context(), args[0], replace)
		},
	}

	cmd.Flags().BoolVar(&replace, "replace", false,
		"mevcut kayıtların yerine geç (dosyada olmayan kayıtlar silinir)")

	return cmd
}

func runImport(ctx context.Context, path string, replace bool) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dosya açılamadı: %w", err)
	}
	// Closing a file opened for reading cannot lose data.
	defer func() { _ = file.Close() }()

	parsed, err := bulk.Import(file)
	if err != nil {
		return err
	}

	// Report the bad rows before doing anything, so the user can abort.
	if len(parsed.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "%d satır atlandı:\n", len(parsed.Errors))
		for _, rowErr := range parsed.Errors {
			fmt.Fprintf(os.Stderr, "  %s\n", rowErr)
		}
		fmt.Fprintln(os.Stderr)
	}

	if len(parsed.Records) == 0 {
		return &exitCodeError{code: exitError, msg: "uygulanabilir kayıt yok"}
	}

	fmt.Printf("%d kayıt uygulanacak.\n", len(parsed.Records))

	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	if replace && cfg.Behaviour.ConfirmDestructive && !flags.dryRun {
		ok, err := confirm("--replace verildi: dosyada olmayan kayıtlar silinecek. Devam edilsin mi?")
		if err != nil {
			return err
		}

		if !ok {
			fmt.Println("İşlem iptal edildi.")
			return nil
		}
	}

	runner := transport.NewSSHRunner(cfg.SSH)

	opts := unbound.WriteOptions{
		Backup: cfg.Behaviour.BackupBeforeWrite,
		DryRun: flags.dryRun,
	}

	results := unbound.Apply(ctx, runner, cfg, opts, func(f *records.File) error {
		return applyImport(f, parsed.Records, replace)
	})

	if err := reportBulkResults(results, opts.DryRun); err != nil {
		return err
	}

	// Rows that failed to parse are still a partial failure, even when every
	// server accepted the rest.
	if len(parsed.Errors) > 0 {
		if err := refreshChanged(ctx, runner, cfg, results); err != nil {
			return err
		}

		return &exitCodeError{
			code: exitPartialError,
			msg:  fmt.Sprintf("%d satır atlandı", len(parsed.Errors)),
		}
	}

	return refreshChanged(ctx, runner, cfg, results)
}

// applyImport adds every record, replacing the existing set when asked.
// An already-present record is not an error here: importing the same file
// twice should converge rather than fail.
func applyImport(f *records.File, recs []records.Record, replace bool) error {
	if replace {
		for _, existing := range f.All() {
			f.Delete(existing.Name, existing.Type, "")
		}
	}

	for _, rec := range recs {
		if err := f.Add(rec); err != nil {
			if _, exists := errors.AsType[*records.ErrExists](err); exists {
				// Update it instead, so an import can also change values.
				if err := f.Update(rec); err != nil {
					return err
				}

				continue
			}

			return err
		}
	}

	if replace {
		f.PruneUnusedZones()
	}

	return nil
}

func reportBulkResults(results []unbound.WriteResult, dryRun bool) error {
	if dryRun {
		fmt.Println("(dry-run: hiçbir değişiklik yapılmadı)")
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
			fmt.Printf("[%s] değişiklik yok\n", label)

		default:
			fmt.Printf("[%s] %d satır eklendi, %d satır silindi\n",
				label, len(res.Diff.Added), len(res.Diff.Removed))
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

func newExportCommand() *cobra.Command {
	var typeName string

	cmd := &cobra.Command{
		Use:   "export [dosya.csv]",
		Short: "Kayıtları CSV olarak dışa aktarır",
		Long: "Dosya adı verilmezse çıktı ekrana yazılır.\n" +
			"Üretilen dosya doğrudan import ile geri yüklenebilir.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			target := ""
			if len(args) == 1 {
				target = args[0]
			}

			return runExport(cmd.Context(), target, typeName)
		},
	}

	cmd.Flags().StringVar(&typeName, "type", "", "yalnızca bu tipteki kayıtları aktar")

	return cmd
}

func runExport(ctx context.Context, target, typeName string) error {
	var wantType records.Type

	if typeName != "" {
		parsed, err := records.ParseType(typeName)
		if err != nil {
			return err
		}
		wantType = parsed
	}

	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	runner := transport.NewSSHRunner(cfg.SSH)
	results := unbound.ReadAll(ctx, runner, cfg)

	recs := mergeRecords(results, wantType)

	if target == "" {
		if err := bulk.Export(os.Stdout, recs); err != nil {
			return err
		}

		return reportReadFailures(results)
	}

	if err := writeExportFile(target, recs); err != nil {
		return err
	}

	fmt.Printf("%d kayıt %s dosyasına yazıldı.\n", len(recs), target)

	return reportReadFailures(results)
}

// writeExportFile writes the export to disk. The close error is returned
// rather than deferred away, because a failure to flush means the file on disk
// is incomplete and reporting success would be a lie.
func writeExportFile(target string, recs []records.Record) error {
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("dosya oluşturulamadı: %w", err)
	}

	if err := bulk.Export(file, recs); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("%s kapatılamadı, dosya eksik olabilir: %w", target, err)
	}

	return nil
}

// mergeRecords collects the union of records across servers, de-duplicated by
// name and type. Exporting the same record once per server would make the file
// unusable as an import source.
func mergeRecords(results []unbound.ServerRecords, wantType records.Type) []records.Record {
	seen := make(map[string]records.Record)

	for _, res := range results {
		for _, rec := range res.Records() {
			if wantType != "" && rec.Type != wantType {
				continue
			}

			if _, exists := seen[rec.Key()]; !exists {
				seen[rec.Key()] = rec
			}
		}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]records.Record, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}

	return out
}
