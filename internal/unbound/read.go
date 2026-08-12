package unbound

import (
	"context"
	"fmt"
	"strings"

	"github.com/kerem/unbound-dns/internal/config"
	"github.com/kerem/unbound-dns/internal/records"
	"github.com/kerem/unbound-dns/internal/transport"
)

// ReadFile fetches and parses the records file from a server.
func ReadFile(ctx context.Context, r transport.Runner, srv config.Server) (*records.File, error) {
	cmd := transport.WithSudo(srv, "cat "+srv.RecordsFile)

	res, err := r.Run(ctx, srv, cmd)
	if err != nil {
		return nil, err
	}

	if !res.Success() {
		return nil, fmt.Errorf("%s okunamadı: %s", srv.RecordsFile, describeReadFailure(res))
	}

	file, err := records.Parse([]byte(res.Stdout))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", srv.RecordsFile, err)
	}

	return file, nil
}

// describeReadFailure turns cat's stderr into something actionable. A missing
// file is the common case and deserves its own hint, because the file has to
// exist for the include directive in unbound.conf to resolve.
func describeReadFailure(res transport.Result) string {
	detail := strings.TrimSpace(res.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.Stdout)
	}

	lower := strings.ToLower(detail)

	switch {
	case strings.Contains(lower, "no such file"):
		return detail + " (dosya yoksa unbound.conf içindeki include de çözülemez, dosyayı oluşturun)"
	case strings.Contains(lower, "permission denied"):
		return detail + " (sudo ayarını kontrol edin)"
	case detail == "":
		return fmt.Sprintf("çıkış kodu %d", res.ExitCode)
	default:
		return detail
	}
}

// ServerRecords pairs a server with what was read from it.
type ServerRecords struct {
	Server config.Server
	File   *records.File
	Err    error
}

// Records returns the parsed records, or nothing when the read failed.
func (s ServerRecords) Records() []records.Record {
	if s.Err != nil || s.File == nil {
		return nil
	}

	return s.File.All()
}

// ReadAll fetches the records file from every configured server.
func ReadAll(ctx context.Context, r transport.Runner, cfg config.Config) []ServerRecords {
	return ForEachServer(ctx, cfg, func(ctx context.Context, srv config.Server) ServerRecords {
		file, err := ReadFile(ctx, r, srv)
		return ServerRecords{Server: srv, File: file, Err: err}
	})
}
