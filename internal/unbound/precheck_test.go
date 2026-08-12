package unbound

import (
	"errors"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/KilimcininKorOglu/yada/internal/records"
)

// serverHolding builds a read result for a server whose file contains body.
func serverHolding(t *testing.T, label, body string) ServerRecords {
	t.Helper()

	file, err := records.Parse([]byte(body))
	if err != nil {
		t.Fatalf("ayrıştırma hatası: %v", err)
	}

	return ServerRecords{Server: config.Server{Name: label}, File: file}
}

func serverUnreadable(label string) ServerRecords {
	return ServerRecords{
		Server: config.Server{Name: label},
		Err:    errors.New("bağlanılamadı"),
	}
}

const (
	holdsTheRecord      = `         local-data: "mail.google.com. IN A 10.10.10.10"` + "\n"
	holdsAnotherValue   = `         local-data: "mail.google.com. IN A 10.20.20.20"` + "\n"
	holdsAnotherTTL     = `         local-data: "mail.google.com. 60 IN A 10.10.10.10"` + "\n"
	holdsSomethingElse  = `         local-data: "www.google.com. IN A 10.30.30.30"` + "\n"
	holdsTheValueByName = `         local-data: "başka.google.com. IN A 10.10.10.10"` + "\n"
)

func TestClassifyAdd(t *testing.T) {
	rec, err := records.New("mail.google.com", records.TypeA, "10.10.10.10", nil)
	if err != nil {
		t.Fatalf("kayıt oluşturulamadı: %v", err)
	}

	cases := []struct {
		name           string
		results        []ServerRecords
		want           AddOutcome
		wantReadable   int
		wantUnreadable int
	}{
		{
			name:         "hiçbir sunucuda yoksa yazılabilir",
			results:      []ServerRecords{serverHolding(t, "ns1", holdsSomethingElse)},
			want:         AddWritable,
			wantReadable: 1,
		},
		{
			// The same address under another name is legal in DNS, so it must
			// not be treated as a collision.
			name:         "aynı değer başka bir adda ise yazılabilir",
			results:      []ServerRecords{serverHolding(t, "ns1", holdsTheValueByName)},
			want:         AddWritable,
			wantReadable: 1,
		},
		{
			name: "bir sunucuda farklı değer varsa çakışma",
			results: []ServerRecords{
				serverHolding(t, "ns1", holdsAnotherValue),
				serverHolding(t, "ns2", holdsSomethingElse),
			},
			want:         AddConflict,
			wantReadable: 2,
		},
		{
			name: "hepsinde birebir aynıysa yineleme",
			results: []ServerRecords{
				serverHolding(t, "ns1", holdsTheRecord),
				serverHolding(t, "ns2", holdsTheRecord),
			},
			want:         AddDuplicate,
			wantReadable: 2,
		},
		{
			// Nothing the user typed gets replaced, so this needs a write but
			// not a question.
			name:         "değer aynı TTL farklıysa yazılabilir",
			results:      []ServerRecords{serverHolding(t, "ns1", holdsAnotherTTL)},
			want:         AddWritable,
			wantReadable: 1,
		},
		{
			name: "bir sunucuda varsa diğerinde yoksa yazılabilir",
			results: []ServerRecords{
				serverHolding(t, "ns1", holdsTheRecord),
				serverHolding(t, "ns2", holdsSomethingElse),
			},
			want:         AddWritable,
			wantReadable: 2,
		},
		{
			name: "okunamayan sunucu yineleme kararını bozmaz",
			results: []ServerRecords{
				serverHolding(t, "ns1", holdsTheRecord),
				serverUnreadable("ns2"),
			},
			want:           AddDuplicate,
			wantReadable:   1,
			wantUnreadable: 1,
		},
		{
			name:           "hiçbir sunucu okunamazsa yazılabilir sayılır",
			results:        []ServerRecords{serverUnreadable("ns1")},
			want:           AddWritable,
			wantUnreadable: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAdd(rec, tc.results)

			if got.Outcome != tc.want {
				t.Errorf("sonuç = %v, beklenen %v", got.Outcome, tc.want)
			}
			if got.Readable != tc.wantReadable {
				t.Errorf("okunabilen = %d, beklenen %d", got.Readable, tc.wantReadable)
			}
			if got.Unreadable != tc.wantUnreadable {
				t.Errorf("okunamayan = %d, beklenen %d", got.Unreadable, tc.wantUnreadable)
			}
			if len(got.States) != len(tc.results) {
				t.Errorf("%d sunucu durumu döndü, %d olmalı", len(got.States), len(tc.results))
			}
		})
	}
}

// The user has to see which server holds what before deciding, so every server
// appears in the summary, in configuration order.
func TestAddCheckSummaryNamesEveryServer(t *testing.T) {
	rec, _ := records.New("mail.google.com", records.TypeA, "10.10.10.10", nil)

	check := classifyAdd(rec, []ServerRecords{
		serverHolding(t, "ns1", holdsAnotherValue),
		serverHolding(t, "ns2", holdsSomethingElse),
		serverHolding(t, "ns3", holdsTheRecord),
		serverUnreadable("ns4"),
	})

	lines := strings.Split(check.Summary(), "\n")

	want := []string{
		"ns1: 10.20.20.20",
		"ns2: kayıt yok",
		"ns3: 10.10.10.10 (aynı)",
		"ns4: okunamadı (bağlanılamadı)",
	}

	if len(lines) != len(want) {
		t.Fatalf("%d satır üretildi, %d olmalı:\n%s", len(lines), len(want), check.Summary())
	}

	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("satır %d = %q, beklenen %q", i+1, lines[i], want[i])
		}
	}
}
