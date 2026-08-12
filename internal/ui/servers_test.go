//go:build !nogui

package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/KilimcininKorOglu/yada/internal/unbound"
)

func serversConfig() config.Config {
	return config.Config{Servers: []config.Server{
		{Name: "ns1", Host: "192.0.2.4", User: "user01"},
		{Name: "ns2", Host: "192.0.2.5", User: "user01", Port: 2222},
	}}
}

// The configured servers are known without touching the network, so the table
// has to list them before anything has been tested.
func TestServerRowsListsEveryConfiguredServerWithoutAStatus(t *testing.T) {
	rows := serverRows(serversConfig(), nil)

	if len(rows) != 2 {
		t.Fatalf("%d satır üretildi, 2 olmalı", len(rows))
	}

	if rows[0].cell(0) != "ns1" {
		t.Errorf("ad sütunu = %q", rows[0].cell(0))
	}
	if rows[0].cell(1) != "user01@192.0.2.4" {
		t.Errorf("adres sütunu = %q", rows[0].cell(1))
	}

	// An untested server must say so rather than look like a failure.
	for col := 2; col < len(serverColumns); col++ {
		if got := rows[0].cell(col); got != "denenmedi" {
			t.Errorf("sütun %d = %q, denenmedi olmalıydı", col, got)
		}
	}
}

func TestServerRowsAttachesTheMatchingStatus(t *testing.T) {
	cfg := serversConfig()

	statuses := []unbound.Status{{
		Server:      cfg.Servers[0],
		Reachable:   true,
		ConfigValid: true,
	}}

	rows := serverRows(cfg, statuses)

	if rows[0].cell(2) != "tamam" {
		t.Errorf("test edilen sunucunun bağlantı sütunu = %q", rows[0].cell(2))
	}

	// The second server was not part of that test and must not inherit a
	// result from the first.
	if got := rows[1].cell(2); got != "denenmedi" {
		t.Errorf("test edilmeyen sunucunun bağlantı sütunu = %q", got)
	}
}

func TestServerAddress(t *testing.T) {
	cases := []struct {
		name   string
		server config.Server
		want   string
	}{
		{"kullanıcı ve port", config.Server{Host: "192.0.2.4", User: "user01", Port: 2222}, "user01@192.0.2.4:2222"},
		{"port yoksa yazılmaz", config.Server{Host: "192.0.2.4", User: "user01"}, "user01@192.0.2.4"},
		{"kullanıcı yoksa yazılmaz", config.Server{Host: "192.0.2.4"}, "192.0.2.4"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverAddress(tc.server); got != tc.want {
				t.Errorf("adres = %q, beklenen %q", got, tc.want)
			}
		})
	}
}

// An empty configuration has to name the way out, since the table shows
// nothing to act on.
func TestDescribeServersPointsAtTheFormWhenThereAreNone(t *testing.T) {
	got := describeServers(config.Config{})

	if !strings.Contains(got, "Sunucu ekle") {
		t.Errorf("özet formu göstermiyor: %q", got)
	}
}

func TestDescribeServersCountsTheConfiguredServers(t *testing.T) {
	got := describeServers(serversConfig())

	if !strings.Contains(got, "2 sunucu") {
		t.Errorf("özet sunucu sayısını vermiyor: %q", got)
	}
}

// The picker has to offer the configured servers before anything has been
// read, since which servers exist is not a question for the network.
func TestServerLabelsListsEveryConfiguredServer(t *testing.T) {
	got := serverLabels(serversConfig())

	want := []string{"ns1", "ns2"}
	if !slices.Equal(got, want) {
		t.Errorf("etiketler = %v, beklenen %v", got, want)
	}
}
