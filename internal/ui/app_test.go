//go:build !nogui

package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// run() cancels its own context once the work returns, so asking the context
// afterwards would call every failure a cancellation and drop the message the
// user needs.
func TestRunReportsAFailureRatherThanACancellation(t *testing.T) {
	a := newTestApp(t, filepath.Join(t.TempDir(), "yada.conf"))

	done := make(chan struct{})

	a.run("test", func(context.Context) error {
		return errors.New("ayar dosyası geçersiz olurdu")
	}, func() { close(done) })

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("iş tamamlanmadı")
	}

	text := a.log.text()

	if !strings.Contains(text, "ayar dosyası geçersiz olurdu") {
		t.Errorf("gerçek hata günlüğe yazılmadı:\n%s", text)
	}
	if strings.Contains(text, "İşlem iptal edildi") {
		t.Errorf("hata iptal olarak bildirildi:\n%s", text)
	}
}
