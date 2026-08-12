// Command unbound-dns-gui is the desktop interface for managing local DNS
// records on Unbound servers.
//
// It is a separate binary from the CLI on purpose: Fyne needs cgo, and folding
// it into the CLI would cost that binary its static, trivially cross-compiled
// build.
package main

import "github.com/kerem/unbound-dns/internal/ui"

// version is set at build time through -ldflags.
var version = "dev"

func main() {
	ui.Run(version)
}
