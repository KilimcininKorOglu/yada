package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errNeedsYes is returned when approval is required but no answer can be
// obtained, which is the case in any non-interactive run.
var errNeedsYes = errors.New("onay gerekiyor ama girdi alınamıyor, --yes kullanın")

// confirm asks the user to approve a destructive action. It returns true when
// the action may proceed.
//
// Automation must not hang on a prompt, so a non-interactive stdin is treated
// as a refusal rather than as silent approval; --yes is the way to say yes
// without a terminal.
func confirm(prompt string) (bool, error) {
	if flags.assumeYes {
		return true, nil
	}

	fmt.Printf("%s (e/H) ", prompt)

	reader := bufio.NewReader(os.Stdin)

	answer, err := reader.ReadString('\n')

	// A character device is not necessarily a terminal: /dev/null passes the
	// mode check and then reads EOF immediately. Treat that as "no answer
	// available" rather than as a read failure.
	if errors.Is(err, io.EOF) && strings.TrimSpace(answer) == "" {
		fmt.Println()
		return false, errNeedsYes
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("onay okunamadı: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "e", "evet", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
