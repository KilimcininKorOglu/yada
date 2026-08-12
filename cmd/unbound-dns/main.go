// Command unbound-dns manages local DNS records on Unbound servers over SSH.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run())
}

func run() int {
	root := newRootCommand()

	if err := root.Execute(); err != nil {
		// A configuration failure has already printed its own detail, so only
		// errors carrying a message are reported here.
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "Hata:", msg)
		}

		return codeOf(err)
	}

	return exitOK
}
