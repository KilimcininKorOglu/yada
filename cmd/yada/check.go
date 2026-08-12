package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/KilimcininKorOglu/yada/internal/transport"
	"github.com/KilimcininKorOglu/yada/internal/unbound"
	"github.com/spf13/cobra"
)

func newCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Sunuculara bağlanır ve config doğrulamasını çalıştırır",
		Long: "Her sunucuya bağlanır, unbound-checkconf ile ana config'i doğrular ve\n" +
			"hangi yenileme yönteminin kullanılabildiğini raporlar.\n" +
			"Hiçbir değişiklik yapmaz.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return runCheck(cmd.Context())
		},
	}
}

func runCheck(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	runner := transport.NewSSHRunner(cfg.SSH)
	statuses := unbound.CheckAll(ctx, runner, cfg)

	if flags.jsonOutput {
		return printCheckJSON(statuses)
	}

	if err := printCheckTable(cfg.SourcePath, statuses); err != nil {
		return err
	}

	if failed := countFailures(statuses); failed > 0 {
		return &exitCodeError{
			code: exitCodeFor(failed, len(statuses)),
			msg:  fmt.Sprintf("%d sunucuda sorun var", failed),
		}
	}

	return nil
}

func printCheckTable(source string, statuses []unbound.Status) error {
	fmt.Printf("Ayar dosyası: %s\n\n", source)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// tabwriter buffers, so a write failure surfaces at Flush; checking there
	// covers the lines written above it too.
	_, _ = fmt.Fprintln(w, "SUNUCU\tBAĞLANTI\tSERVİS\tCONFIG\tYENİLEME")

	for _, st := range statuses {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			st.Server.Label(),
			mark(st.Reachable),
			serviceMark(st),
			configMark(st),
			st.AvailableTier(),
		)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("çıktı yazılamadı: %w", err)
	}

	printCheckDetails(statuses)

	return nil
}

func printCheckDetails(statuses []unbound.Status) {
	for _, st := range statuses {
		switch {
		case st.Err != nil:
			fmt.Printf("\n[%s] Bağlanılamadı:\n  %s\n", st.Server.Label(), st.Err)

		case !st.ConfigValid:
			fmt.Printf("\n[%s] Config doğrulaması başarısız:\n", st.Server.Label())
			printIndented(st.ConfigOutput)

		case !st.ControlAvailable && st.ControlOutput != "":
			// Not an error on its own, the tool falls back to a lighter tier.
			fmt.Printf("\n[%s] unbound-control kullanılamıyor, %s kullanılacak:\n",
				st.Server.Label(), st.AvailableTier())
			printIndented(st.ControlOutput)
		}
	}
}

func printIndented(text string) {
	if strings.TrimSpace(text) == "" {
		fmt.Println("  (çıktı yok)")
		return
	}

	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
}

func printCheckJSON(statuses []unbound.Status) error {
	type entry struct {
		Server           string `json:"server"`
		Host             string `json:"host"`
		Reachable        bool   `json:"reachable"`
		ServiceActive    bool   `json:"service_active"`
		ConfigValid      bool   `json:"config_valid"`
		ConfigOutput     string `json:"config_output,omitempty"`
		ControlAvailable bool   `json:"control_available"`
		SignalReload     bool   `json:"signal_reload_available"`
		Tier             string `json:"refresh_tier"`
		Version          string `json:"version,omitempty"`
		Error            string `json:"error,omitempty"`
	}

	out := make([]entry, 0, len(statuses))

	for _, st := range statuses {
		e := entry{
			Server:           st.Server.Label(),
			Host:             st.Server.Host,
			Reachable:        st.Reachable,
			ServiceActive:    st.ServiceActive,
			ConfigValid:      st.ConfigValid,
			ConfigOutput:     st.ConfigOutput,
			ControlAvailable: st.ControlAvailable,
			SignalReload:     st.SignalReloadAvailable,
			Tier:             st.AvailableTier(),
			Version:          st.Version,
		}

		if st.Err != nil {
			e.Error = st.Err.Error()
		}

		out = append(out, e)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(out)
}

func countFailures(statuses []unbound.Status) int {
	failed := 0

	for _, st := range statuses {
		if !st.Reachable || !st.ConfigValid {
			failed++
		}
	}

	return failed
}

// exitCodeFor distinguishes a total failure from a partial one, so automation
// can tell "nothing worked" from "one host out of three is down".
func exitCodeFor(failed, total int) int {
	if failed == total {
		return exitError
	}
	return exitPartialError
}

func mark(ok bool) string {
	if ok {
		return "tamam"
	}
	return "HATA"
}

func serviceMark(st unbound.Status) string {
	switch {
	case !st.Reachable:
		return "-"
	case st.ServiceActive:
		return "aktif"
	default:
		return "PASİF"
	}
}

func configMark(st unbound.Status) string {
	switch {
	case !st.Reachable:
		return "-"
	case st.ConfigValid:
		return "geçerli"
	default:
		return "GEÇERSİZ"
	}
}
