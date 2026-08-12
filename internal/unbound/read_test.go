package unbound

import (
	"context"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/yada/internal/transport"
)

const sampleRecords = `# Yerel kayıtlar
         local-zone: "google.com." transparent
         local-data: "mail.google.com. IN A 10.10.10.10"
`

func TestReadFileParsesRecords(t *testing.T) {
	r := &fakeRunner{
		replies: map[string]transport.Result{
			"cat ": {Stdout: sampleRecords},
		},
	}

	file, err := ReadFile(context.Background(), r, testServer())
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	all := file.All()
	if len(all) != 1 {
		t.Fatalf("%d kayıt bulundu, beklenen 1", len(all))
	}
	if all[0].Name != "mail.google.com." {
		t.Errorf("ad = %q", all[0].Name)
	}
}

func TestReadFileReadsConfiguredPath(t *testing.T) {
	r := &fakeRunner{replies: map[string]transport.Result{"cat ": {Stdout: ""}}}

	if _, err := ReadFile(context.Background(), r, testServer()); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}

	if !r.sawCommandContaining("cat /etc/unbound/local_records.conf") {
		t.Error("kayıt dosyası ayardaki yoldan okunmadı")
	}
}

// A missing records file breaks the include in unbound.conf, so the message
// has to say more than "no such file".
func TestReadFileExplainsMissingFile(t *testing.T) {
	r := &fakeRunner{
		replies: map[string]transport.Result{
			"cat ": {
				Stderr:   "cat: /etc/unbound/local_records.conf: No such file or directory",
				ExitCode: 1,
			},
		},
	}

	_, err := ReadFile(context.Background(), r, testServer())
	if err == nil {
		t.Fatal("eksik dosya hata vermedi")
	}
	if !strings.Contains(err.Error(), "include") {
		t.Errorf("hata include uyarısı içermiyor: %v", err)
	}
}

func TestReadFileExplainsPermissionDenied(t *testing.T) {
	r := &fakeRunner{
		replies: map[string]transport.Result{
			"cat ": {
				Stderr:   "cat: /etc/unbound/local_records.conf: Permission denied",
				ExitCode: 1,
			},
		},
	}

	_, err := ReadFile(context.Background(), r, testServer())
	if err == nil {
		t.Fatal("izin hatası bildirilmedi")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("hata sudo yönlendirmesi içermiyor: %v", err)
	}
}

func TestServerRecordsHidesRecordsOnError(t *testing.T) {
	sr := ServerRecords{Err: context.Canceled}

	if got := sr.Records(); got != nil {
		t.Errorf("hatalı okumadan kayıt döndü: %v", got)
	}
}
