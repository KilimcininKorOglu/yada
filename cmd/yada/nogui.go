//go:build nogui

package main

import (
	"fmt"
	"os"
)

// runGUI reports that this build has no desktop interface.
//
// The nogui build exists so the command line ships as a static binary that
// cross-compiles to every platform in one step. Failing here with an
// explanation is better than silently doing nothing when someone runs it
// without arguments and expects a window.
func runGUI([]string) int {
	fmt.Fprintln(os.Stderr, "Bu yapı arayüzsüz derlendi (nogui).")
	fmt.Fprintln(os.Stderr, "Komutları listelemek için: yada --help")

	return exitError
}
