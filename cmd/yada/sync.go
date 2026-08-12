package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/kerem/yada/internal/config"
	"github.com/kerem/yada/internal/diff"
	"github.com/kerem/yada/internal/records"
	"github.com/kerem/yada/internal/transport"
	"github.com/kerem/yada/internal/unbound"
	"github.com/spf13/cobra"
)

func newDiffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Sunucular arasındaki kayıt farklarını gösterir",
		Long: "Kayıtları ad ve tipe göre karşılaştırır. TTL farkı fark sayılmaz,\n" +
			"çünkü çözümleyicinin verdiği cevabı değiştirmez.\n\n" +
			"Aynı ad ve tip için farklı değer tutan sunucular çakışma olarak\n" +
			"işaretlenir ve otomatik eşitlenmez.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return runDiff(cmd.Context())
		},
	}
}

func runDiff(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	if len(cfg.Servers) < 2 {
		return fmt.Errorf("karşılaştırma için en az iki sunucu gerekir, %d tanımlı", len(cfg.Servers))
	}

	runner := transport.NewSSHRunner(cfg.SSH)
	results := unbound.ReadAll(ctx, runner, cfg)

	sets, err := serverSets(results)
	if err != nil {
		return err
	}

	comparison := diff.Compare(sets)

	if comparison.InSync() {
		fmt.Printf("Sunucular eşit (%d kayıt).\n", len(comparison.Entries))
		return reportReadFailures(results)
	}

	if err := printDiff(comparison); err != nil {
		return err
	}

	return reportReadFailures(results)
}

// serverSets turns read results into comparable sets, refusing to compare when
// a server could not be read: a missing file would otherwise look like a
// server with no records, which reads as "everything is missing here".
func serverSets(results []unbound.ServerRecords) ([]diff.ServerSet, error) {
	var (
		sets   []diff.ServerSet
		failed []string
	)

	for _, res := range results {
		if res.Err != nil {
			failed = append(failed, res.Server.Label())
			continue
		}

		sets = append(sets, diff.ServerSet{
			Label:   res.Server.Label(),
			Records: res.Records(),
		})
	}

	if len(failed) > 0 {
		return nil, &exitCodeError{
			code: exitError,
			msg: fmt.Sprintf(
				"%s okunamadığı için karşılaştırma yapılamaz (okunamayan sunucu boş sanılırdı)",
				strings.Join(failed, ", ")),
		}
	}

	if len(sets) < 2 {
		return nil, &exitCodeError{code: exitError, msg: "karşılaştırma için en az iki okunabilir sunucu gerekir"}
	}

	return sets, nil
}

func printDiff(comparison diff.Result) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(w, "AD\tTİP\tDURUM\tAYRINTI")

	for _, entry := range comparison.Entries {
		if entry.Status == diff.StatusSame {
			continue
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			strings.TrimSuffix(entry.Record.Name, "."),
			entry.Record.Type,
			entry.Status,
			describeEntry(entry),
		)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("çıktı yazılamadı: %w", err)
	}

	conflicts := comparison.Conflicts()
	if len(conflicts) > 0 {
		fmt.Printf("\n%d çakışma var, bunlar otomatik eşitlenmez:\n", len(conflicts))

		for _, entry := range conflicts {
			fmt.Printf("  %s %s:\n", strings.TrimSuffix(entry.Record.Name, "."), entry.Record.Type)

			for _, server := range entry.Present {
				fmt.Printf("    %s: %s\n", server, entry.Values[server])
			}
		}

		fmt.Println("\nDoğru değeri seçtikten sonra: yada update <ad> --type <tip> --value <değer>")
	}

	return nil
}

func describeEntry(entry diff.Entry) string {
	switch entry.Status {
	case diff.StatusMissing:
		return fmt.Sprintf("%s sunucusunda yok", strings.Join(entry.Missing, ", "))
	case diff.StatusConflict:
		parts := make([]string, 0, len(entry.Present))
		for _, server := range entry.Present {
			parts = append(parts, fmt.Sprintf("%s=%s", server, entry.Values[server]))
		}

		return strings.Join(parts, "  ")
	default:
		return ""
	}
}

func newSyncCommand() *cobra.Command {
	var (
		from  string
		prune bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Bir sunucuyu referans alarak diğerlerini eşitler",
		Long: "Kaynak sunucuda olup hedeflerde olmayan kayıtları ekler.\n" +
			"Hedefte fazladan bulunan kayıtlar varsayılan olarak silinmez;\n" +
			"silmek için --prune verin.\n\n" +
			"Çakışan kayıtlar atlanır ve listelenir, çünkü hangi değerin doğru\n" +
			"olduğuna karar vermek kullanıcıya aittir.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true

			if from == "" {
				return fmt.Errorf("--from zorunlu (kaynak sunucunun adı veya host'u)")
			}

			return runSync(cmd.Context(), from, prune)
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "referans alınacak sunucu")
	cmd.Flags().BoolVar(&prune, "prune", false, "kaynakta olmayan kayıtları hedeflerden sil")

	return cmd
}

func runSync(ctx context.Context, from string, prune bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	sourceServer, err := findServer(cfg, from)
	if err != nil {
		return err
	}

	runner := transport.NewSSHRunner(cfg.SSH)
	results := unbound.ReadAll(ctx, runner, cfg)

	sets, err := serverSets(results)
	if err != nil {
		return err
	}

	source, targets := splitSource(sets, sourceServer.Label())
	if len(targets) == 0 {
		return fmt.Errorf("eşitlenecek hedef sunucu yok")
	}

	plans, conflicts := diff.PlanSync(source, targets, prune)

	printSyncPlan(source.Label, plans, conflicts, prune)

	if allEmpty(plans) {
		return reportSyncConflicts(conflicts)
	}

	if flags.dryRun {
		fmt.Println("\n(dry-run: hiçbir değişiklik yapılmadı)")
		return reportSyncConflicts(conflicts)
	}

	if cfg.Behaviour.ConfirmDestructive && hasRemovals(plans) {
		ok, err := confirm("Bu değişiklikler uygulansın mı?")
		if err != nil {
			return err
		}

		if !ok {
			fmt.Println("İşlem iptal edildi.")
			return nil
		}
	}

	writes, err := applySync(ctx, runner, cfg, sourceServer, plans, prune)
	if err != nil {
		return err
	}

	if err := refreshChanged(ctx, runner, cfg, writes); err != nil {
		return err
	}

	return reportSyncConflicts(conflicts)
}

func findServer(cfg config.Config, name string) (config.Server, error) {
	for _, srv := range cfg.Servers {
		if srv.Name == name || srv.Host == name {
			return srv, nil
		}
	}

	labels := make([]string, len(cfg.Servers))
	for i, srv := range cfg.Servers {
		labels[i] = srv.Label()
	}

	return config.Server{}, fmt.Errorf("bilinmeyen sunucu %q (tanımlı olanlar: %s)", name, strings.Join(labels, ", "))
}

func splitSource(sets []diff.ServerSet, sourceLabel string) (diff.ServerSet, []diff.ServerSet) {
	var (
		source  diff.ServerSet
		targets []diff.ServerSet
	)

	for _, set := range sets {
		if set.Label == sourceLabel {
			source = set
			continue
		}

		targets = append(targets, set)
	}

	return source, targets
}

func printSyncPlan(sourceLabel string, plans []diff.Plan, conflicts []diff.Entry, prune bool) {
	fmt.Printf("Kaynak: %s\n", sourceLabel)

	if !prune {
		fmt.Println("(--prune verilmedi, hedeflerde fazladan olan kayıtlar silinmeyecek)")
	}

	fmt.Println()

	for _, plan := range plans {
		if plan.Empty() {
			fmt.Printf("[%s] eşit, yapılacak bir şey yok\n", plan.Server)
			continue
		}

		fmt.Printf("[%s]\n", plan.Server)

		for _, rec := range plan.Add {
			fmt.Printf("    + %s\n", rec.String())
		}
		for _, rec := range plan.Remove {
			fmt.Printf("    - %s\n", rec.String())
		}
	}

	if len(conflicts) > 0 {
		fmt.Printf("\n%d çakışan kayıt atlandı.\n", len(conflicts))
	}
}

func applySync(
	ctx context.Context,
	runner transport.Runner,
	cfg config.Config,
	source config.Server,
	plans []diff.Plan,
	prune bool,
) ([]unbound.WriteResult, error) {
	byServer := make(map[string]diff.Plan, len(plans))
	for _, plan := range plans {
		byServer[plan.Server] = plan
	}

	// The source itself must not be written to, so it is excluded from the
	// set Apply walks.
	scoped := cfg
	scoped.Servers = nil

	for _, srv := range cfg.Servers {
		if srv.Label() == source.Label() {
			continue
		}

		scoped.Servers = append(scoped.Servers, srv)
	}

	opts := unbound.WriteOptions{
		Backup: cfg.Behaviour.BackupBeforeWrite,
		DryRun: flags.dryRun,
	}

	results := unbound.ApplyPerServer(ctx, runner, scoped, opts, func(srv config.Server, f *records.File) error {
		plan := byServer[srv.Label()]

		for _, rec := range plan.Add {
			if err := f.Add(rec); err != nil {
				return err
			}
		}

		for _, rec := range plan.Remove {
			f.Delete(rec.Name, rec.Type, "")
		}

		if prune && len(plan.Remove) > 0 {
			f.PruneUnusedZones()
		}

		return nil
	})

	fmt.Println()

	return results, reportBulkResults(results, opts.DryRun)
}

func allEmpty(plans []diff.Plan) bool {
	for _, plan := range plans {
		if !plan.Empty() {
			return false
		}
	}

	return true
}

func hasRemovals(plans []diff.Plan) bool {
	for _, plan := range plans {
		if len(plan.Remove) > 0 {
			return true
		}
	}

	return false
}

// reportSyncConflicts turns leftover conflicts into a partial-success exit
// code, so an automated sync does not report complete success while records
// still disagree.
func reportSyncConflicts(conflicts []diff.Entry) error {
	if len(conflicts) == 0 {
		return nil
	}

	fmt.Println("\nÇakışmalar eşitlenmedi:")

	for _, entry := range conflicts {
		fmt.Printf("  %s %s:\n", strings.TrimSuffix(entry.Record.Name, "."), entry.Record.Type)

		for _, server := range entry.Present {
			fmt.Printf("    %s: %s\n", server, entry.Values[server])
		}
	}

	return &exitCodeError{
		code: exitPartialError,
		msg:  fmt.Sprintf("%d çakışma elle çözülmeli", len(conflicts)),
	}
}
