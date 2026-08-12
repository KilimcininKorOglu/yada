//go:build !nogui

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// maxLogLines bounds the panel so a long session cannot grow without limit.
// The file, when one is configured, keeps everything.
const maxLogLines = 500

// logPanel shows the remote commands and their outcome. It exists so a user
// reporting a problem can copy exactly what happened rather than describe it.
type logPanel struct {
	mu    sync.Mutex
	lines []string

	// file is the sink named by log.file in the configuration. Without it the
	// panel is the only record, and it is gone when the window closes.
	file     *os.File
	filePath string

	entry  *widget.Entry
	status *widget.Label
}

func newLogPanel() *logPanel {
	entry := widget.NewMultiLineEntry()
	entry.Wrapping = fyne.TextWrapWord

	return &logPanel{
		entry:  entry,
		status: widget.NewLabel(""),
	}
}

// setFile points the log at a file, closing whatever it was writing to before.
// An empty path means the panel is the only record.
//
// A failure is returned rather than swallowed: silently not writing a log the
// user asked for is worse than saying so.
func (l *logPanel) setFile(path string) error {
	l.mu.Lock()

	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
		l.filePath = ""
	}

	if path == "" {
		l.mu.Unlock()
		l.updateStatus()

		return nil
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			l.mu.Unlock()
			return fmt.Errorf("günlük dizini oluşturulamadı (%s): %w", dir, err)
		}
	}

	// The log records which servers exist and what was changed on them, so it
	// is readable only by its owner.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.mu.Unlock()
		return fmt.Errorf("günlük dosyası açılamadı (%s): %w", path, err)
	}

	l.file = file
	l.filePath = path
	l.mu.Unlock()

	l.updateStatus()

	return nil
}

func (l *logPanel) close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

func (l *logPanel) add(line string) {
	l.mu.Lock()

	l.lines = append(l.lines, line)

	sinkDropped := l.writeToFile(line)

	if len(l.lines) > maxLogLines {
		l.lines = l.lines[len(l.lines)-maxLogLines:]
	}

	text := strings.Join(l.lines, "\n")
	l.mu.Unlock()

	// Widget updates must happen on the UI goroutine, and add is called from
	// background work as well.
	fyne.Do(func() {
		l.entry.SetText(text)
		l.entry.CursorRow = len(l.lines)
	})

	if sinkDropped {
		l.updateStatus()
	}
}

// writeToFile appends the entry to the configured sink. The caller must hold
// the lock.
//
// The file carries a timestamp because it outlives the session and is read
// long after the fact. The panel does not, so a copied excerpt stays readable.
//
// A write failure closes the sink and reports itself in the panel: retrying
// every line would bury the real output, and staying quiet would leave the
// user believing a log is being kept when it is not. It returns whether the
// sink was dropped.
func (l *logPanel) writeToFile(line string) bool {
	if l.file == nil {
		return false
	}

	stamp := time.Now().Format("2006-01-02 15:04:05")

	var b strings.Builder
	for entryLine := range strings.SplitSeq(line, "\n") {
		fmt.Fprintf(&b, "%s  %s\n", stamp, entryLine)
	}

	_, err := l.file.WriteString(b.String())
	if err == nil {
		return false
	}

	path := l.filePath

	_ = l.file.Close()
	l.file = nil
	l.filePath = ""

	l.lines = append(l.lines, fmt.Sprintf(
		"UYARI: günlük dosyasına yazılamadı (%s): %v. Dosyaya yazma durduruldu.", path, err))

	return true
}

func (l *logPanel) addf(format string, args ...any) {
	l.add(fmt.Sprintf(format, args...))
}

func (l *logPanel) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return strings.Join(l.lines, "\n")
}

// path reports the file being written to, empty when there is none.
func (l *logPanel) path() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.filePath
}

// clear empties the panel only. The file is an append-only record and is not
// truncated, because a button that quietly destroys history is a trap.
func (l *logPanel) clear() {
	l.mu.Lock()
	l.lines = nil
	l.mu.Unlock()

	fyne.Do(func() {
		l.entry.SetText("")
	})
}

func (l *logPanel) updateStatus() {
	text := "Günlük yalnızca bu pencerede tutuluyor. Dosyaya yazmak için ayarlarda log.file tanımlayın."

	if path := l.path(); path != "" {
		text = "Günlük dosyası: " + path
	}

	fyne.Do(func() {
		l.status.SetText(text)
	})
}

func (l *logPanel) canvas() fyne.CanvasObject {
	copyButton := widget.NewButton("Panoya kopyala", func() {
		fyne.CurrentApp().Clipboard().SetContent(l.text())
	})

	clearButton := widget.NewButton("Temizle", func() {
		l.clear()
	})

	toolbar := container.NewHBox(copyButton, clearButton)

	l.updateStatus()

	return container.NewBorder(toolbar, l.status, nil, nil, l.entry)
}
