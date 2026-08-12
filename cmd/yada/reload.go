package main

import (
	"context"
	"fmt"

	"github.com/kerem/yada/internal/config"
	"github.com/kerem/yada/internal/transport"
	"github.com/kerem/yada/internal/unbound"
	"github.com/spf13/cobra"
)

func newReloadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Sunuculardaki Unbound servisine yeni config'i okutur",
		Long: "En hafif yöntemden başlayarak sırayla dener:\n" +
			"  1. unbound-control reload_keep_cache  (kesinti yok, cache korunur)\n" +
			"  2. systemctl reload                   (kesinti yok, cache temizlenir)\n" +
			"  3. systemctl restart                  (kısa kesinti, cache temizlenir)\n\n" +
			"Bir kademe başarısız olursa bir sonrakine geçer ve nedenini yazar.\n" +
			"Ayar dosyasındaki reload_strategy tek bir kademeye sabitleyebilir.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return runReload(cmd.Context())
		},
	}
}

func runReload(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return &exitCodeError{code: reportConfigError(err)}
	}

	if flags.dryRun {
		fmt.Println("(dry-run: yenileme yapılmadı)")
		fmt.Printf("Strateji: %s\n", cfg.Behaviour.ReloadStrategy)

		for _, srv := range cfg.Servers {
			fmt.Printf("[%s] yenilenecekti\n", srv.Label())
		}

		return nil
	}

	runner := transport.NewSSHRunner(cfg.SSH)
	results := unbound.ReloadAll(ctx, runner, cfg)

	return reportReloadResults(results, cfg.Behaviour.ReloadStrategy)
}

func reportReloadResults(results []unbound.ReloadResult, strategy config.ReloadStrategy) error {
	failed := 0

	for _, res := range results {
		label := res.Server.Label()

		// Show why a lighter tier was skipped, so the cost of the tier that
		// did run is never a surprise.
		for _, attempt := range res.Attempts {
			if attempt.Err == nil {
				continue
			}

			fmt.Printf("[%s] %s kullanılamadı: %s\n", label, attempt.Tier, attempt.Err)

			if attempt.Output != "" {
				printIndented(attempt.Output)
			}
		}

		if res.Err != nil {
			failed++
			fmt.Printf("[%s] BAŞARISIZ: %s\n", label, res.Err)

			continue
		}

		fmt.Printf("[%s] %s ile yenilendi (%s)\n", label, res.Tier, res.Tier.Description())
	}

	if failed > 0 {
		hint := ""
		if strategy != config.ReloadAuto {
			hint = fmt.Sprintf(" (reload_strategy %q olarak sabitlenmiş, alt kademelere düşülmedi)", strategy)
		}

		return &exitCodeError{
			code: exitCodeFor(failed, len(results)),
			msg:  fmt.Sprintf("%d sunucu yenilenemedi%s", failed, hint),
		}
	}

	return nil
}
