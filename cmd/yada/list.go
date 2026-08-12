package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KilimcininKorOglu/yada/internal/records"
	"github.com/KilimcininKorOglu/yada/internal/transport"
	"github.com/KilimcininKorOglu/yada/internal/unbound"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	var (
		typeFilter string
		nameFilter string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Sunuculardaki kayıtları listeler",
		Long: "Her sunucudan kayıt dosyasını okur ve kayıtları birleştirilmiş bir\n" +
			"tabloda gösterir. SUNUCULAR sütunu kaydın hangi sunucularda\n" +
			"bulunduğunu belirtir.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return runList(cmd.Context(), typeFilter, nameFilter)
		},
	}

	cmd.Flags().StringVar(&typeFilter, "type", "", "yalnızca bu kayıt tipini göster (A, AAAA, CNAME, TXT, MX, PTR)")
	cmd.Flags().StringVar(&nameFilter, "filter", "", "adında bu metni içeren kayıtları göster")

	return cmd
}

func runList(ctx context.Context, typeFilter, nameFilter string) error {
	var wantType records.Type

	if typeFilter != "" {
		parsed, err := records.ParseType(typeFilter)
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

	rows := collectRows(results, wantType, nameFilter)

	if flags.jsonOutput {
		if err := printListJSON(rows); err != nil {
			return err
		}
	} else if err := printListTable(rows); err != nil {
		return err
	}

	return reportReadFailures(results)
}

// row is one record as shown to the user, together with the servers holding
// it and whether they agree on its value.
type row struct {
	Record    records.Record
	Servers   []string
	Conflicts map[string]string
}

// Conflicted reports whether the servers disagree about the value.
func (r row) Conflicted() bool {
	return len(r.Conflicts) > 0
}

func collectRows(results []unbound.ServerRecords, wantType records.Type, nameFilter string) []row {
	byKey := make(map[string]*row)
	var order []string

	needle := strings.ToLower(strings.TrimSpace(nameFilter))

	for _, res := range results {
		for _, rec := range res.Records() {
			if wantType != "" && rec.Type != wantType {
				continue
			}
			if needle != "" && !strings.Contains(strings.ToLower(rec.Name), needle) {
				continue
			}

			key := rec.Key()

			existing, seen := byKey[key]
			if !seen {
				byKey[key] = &row{
					Record:    rec,
					Servers:   []string{res.Server.Label()},
					Conflicts: map[string]string{},
				}
				order = append(order, key)
				continue
			}

			existing.Servers = append(existing.Servers, res.Server.Label())

			// Same name and type but a different value means the servers have
			// drifted apart, which the user needs to see rather than have one
			// value silently win.
			if rec.Value != existing.Record.Value {
				existing.Conflicts[res.Server.Label()] = rec.Value
			}
		}
	}

	sort.Strings(order)

	out := make([]row, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}

	return out
}

func printListTable(rows []row) error {
	if len(rows) == 0 {
		fmt.Println("Kayıt bulunamadı.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(w, "AD\tTİP\tDEĞER\tTTL\tSUNUCULAR")

	for _, r := range rows {
		value := r.Record.Value
		if r.Conflicted() {
			value += "  (ÇAKIŞMA)"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			strings.TrimSuffix(r.Record.Name, "."),
			r.Record.Type,
			value,
			formatTTL(r.Record.TTL),
			strings.Join(r.Servers, ", "),
		)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("çıktı yazılamadı: %w", err)
	}

	printConflicts(rows)

	fmt.Printf("\nToplam %d kayıt.\n", len(rows))

	return nil
}

func printConflicts(rows []row) {
	for _, r := range rows {
		if !r.Conflicted() {
			continue
		}

		fmt.Printf("\nÇAKIŞMA: %s %s sunucular arasında farklı:\n",
			strings.TrimSuffix(r.Record.Name, "."), r.Record.Type)

		fmt.Printf("  %s: %s\n", r.Servers[0], r.Record.Value)

		for _, server := range slices.Sorted(maps.Keys(r.Conflicts)) {
			fmt.Printf("  %s: %s\n", server, r.Conflicts[server])
		}
	}
}

func printListJSON(rows []row) error {
	type entry struct {
		Name      string            `json:"name"`
		Type      string            `json:"type"`
		Value     string            `json:"value"`
		TTL       *uint32           `json:"ttl,omitempty"`
		Servers   []string          `json:"servers"`
		Conflicts map[string]string `json:"conflicts,omitempty"`
	}

	out := make([]entry, 0, len(rows))

	for _, r := range rows {
		e := entry{
			Name:    r.Record.Name,
			Type:    string(r.Record.Type),
			Value:   r.Record.Value,
			TTL:     r.Record.TTL,
			Servers: r.Servers,
		}

		if r.Conflicted() {
			e.Conflicts = r.Conflicts
		}

		out = append(out, e)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(out)
}

// reportReadFailures prints the servers that could not be read and turns that
// into an exit code, so a partial listing is never mistaken for a complete one.
func reportReadFailures(results []unbound.ServerRecords) error {
	var failed []unbound.ServerRecords

	for _, res := range results {
		if res.Err != nil {
			failed = append(failed, res)
		}
	}

	if len(failed) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stderr)
	for _, res := range failed {
		fmt.Fprintf(os.Stderr, "[%s] okunamadı: %s\n", res.Server.Label(), res.Err)
	}

	return &exitCodeError{
		code: exitCodeFor(len(failed), len(results)),
		msg:  fmt.Sprintf("%d sunucu okunamadı, liste eksik olabilir", len(failed)),
	}
}

func formatTTL(ttl *uint32) string {
	if ttl == nil {
		return "-"
	}

	return strconv.FormatUint(uint64(*ttl), 10)
}
