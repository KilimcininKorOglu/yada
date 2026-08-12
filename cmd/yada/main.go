// Command yada manages local DNS records on Unbound servers over SSH.
//
// It opens the desktop interface by default. Pass -cli, or any subcommand, to
// use the command line instead.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

// dispatch picks the interface and runs it.
func dispatch(args []string) int {
	useGUI, rest := selectMode(args)

	if useGUI {
		return runGUI(rest)
	}

	return runCLI(rest)
}

// selectMode decides between the two interfaces and returns the arguments the
// chosen one should see.
//
// An explicit choice can only be made with the first argument. Scanning the
// whole list would let a record value such as "-cli" change how the program
// behaves, and a flag buried behind a subcommand belongs to that subcommand.
func selectMode(args []string) (useGUI bool, rest []string) {
	if len(args) == 0 {
		return true, nil
	}

	switch args[0] {
	case "-cli", "--cli":
		return false, args[1:]
	case "-gui", "--gui":
		return true, args[1:]
	}

	// Without an explicit choice, the interface runs when it can account for
	// every argument by itself. Anything it does not understand is a
	// subcommand, --help, or a command-line flag, all of which mean the user
	// wants the command line.
	if _, err := guiArgs(args); err == nil {
		return true, args
	}

	return false, args
}

// runCLI executes the cobra command tree.
func runCLI(args []string) int {
	root := newRootCommand()
	root.SetArgs(args)

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

// guiArgs reads the few flags the desktop interface understands. It has no
// command tree of its own, so anything else is a mistake worth naming rather
// than ignoring.
func guiArgs(args []string) (configPath string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--config bir dosya yolu bekliyor")
			}

			configPath = args[i+1]
			i++

		default:
			if path, found := cutFlag(args[i], "--config="); found {
				configPath = path
				continue
			}

			return "", fmt.Errorf(
				"arayüz %q seçeneğini tanımıyor (arayüz yalnızca --config alır, komutlar için: yada -cli --help)",
				args[i])
		}
	}

	return configPath, nil
}

func cutFlag(arg, prefix string) (value string, found bool) {
	if len(arg) <= len(prefix) || arg[:len(prefix)] != prefix {
		return "", false
	}

	return arg[len(prefix):], true
}
