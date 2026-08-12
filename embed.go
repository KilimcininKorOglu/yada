// Package unbounddns exists only to embed the files that belong at the root of
// the repository.
//
// The embed directive cannot reach outside the directory holding the Go file,
// so a file that must be visible at the root cannot be embedded from a package
// below it.
// Keeping the directive here lets the example configuration sit where someone
// opening the repository will find it, with no second copy to drift.
package unbounddns

import _ "embed"

// ConfExample is the commented starter configuration. It doubles as the
// documentation of every available key, which is why it is shipped inside the
// binary as well: a binary copied to a server can still write a usable file.
//
//go:embed unbound-dns.conf.example
var ConfExample []byte
