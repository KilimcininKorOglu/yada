package sshsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strings"
)

// The OpenSSH client tools are already a hard prerequisite of this
// application, so ssh-keygen and ssh-keyscan are used rather than a Go
// implementation of the same formats. They ship in the same package as ssh,
// they agree with it about every file this package writes, and they add no
// dependency.

// runLocal executes one of the OpenSSH helper tools and returns its output.
//
// name is a constant chosen in this package, never user input, and the
// arguments go straight to execve without a shell, so nothing here can be
// reinterpreted. gosec cannot see that the name is fixed.
func runLocal(ctx context.Context, stdin io.Reader, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	// #nosec G204
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = stdin

	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}

	// ErrNotFound covers a failed PATH lookup; an absolute path that does not
	// exist surfaces as fs.ErrNotExist instead.
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s bulunamadı, OpenSSH istemcisi kurulu olmalı", name)
	}

	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = strings.TrimSpace(stdout.String())
	}

	if detail == "" {
		return "", fmt.Errorf("%s başarısız oldu: %w", name, err)
	}

	return "", fmt.Errorf("%s başarısız oldu: %s", name, detail)
}
