//go:build !nogui

package ui

import (
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// maxLogLines bounds the panel so a long session cannot grow without limit.
const maxLogLines = 500

// logPanel shows the remote commands and their outcome. It exists so a user
// reporting a problem can copy exactly what happened rather than describe it.
type logPanel struct {
	mu    sync.Mutex
	lines []string

	entry *widget.Entry
}

func newLogPanel() *logPanel {
	entry := widget.NewMultiLineEntry()
	entry.Wrapping = fyne.TextWrapWord

	return &logPanel{entry: entry}
}

func (l *logPanel) add(line string) {
	l.mu.Lock()

	l.lines = append(l.lines, line)
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
}

func (l *logPanel) addf(format string, args ...any) {
	l.add(fmt.Sprintf(format, args...))
}

func (l *logPanel) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return strings.Join(l.lines, "\n")
}

func (l *logPanel) clear() {
	l.mu.Lock()
	l.lines = nil
	l.mu.Unlock()

	fyne.Do(func() {
		l.entry.SetText("")
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

	return container.NewBorder(toolbar, nil, nil, nil, l.entry)
}
