//go:build !nogui

package main

import (
	"fmt"
	"os"

	"github.com/kerem/unbound-dns/internal/ui"
)

// runGUI opens the desktop window and blocks until it closes.
//
// This file is excluded by the nogui build tag, which is what produces the
// static command-line-only binary: Fyne needs cgo, and a binary that links it
// cannot be cross-compiled with a single command.
func runGUI(args []string) int {
	configPath, err := guiArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Hata:", err)
		return exitError
	}

	ui.Run(version, configPath)

	return exitOK
}
