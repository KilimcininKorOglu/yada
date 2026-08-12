package records

import (
	"fmt"
	"strings"

	"golang.org/x/net/idna"
)

const (
	maxNameLength  = 253
	maxLabelLength = 63
)

// ValidateName checks a domain name against the DNS rules Unbound enforces.
// The name may carry a trailing dot, which is stripped before the length and
// label checks.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("alan adı boş olamaz")
	}

	ascii, err := ToASCII(trimmed)
	if err != nil {
		return err
	}

	bare := strings.TrimSuffix(ascii, ".")
	if bare == "" {
		return fmt.Errorf("alan adı yalnızca noktadan oluşamaz")
	}

	if len(bare) > maxNameLength {
		return fmt.Errorf("alan adı %d karakter, en fazla %d olabilir", len(bare), maxNameLength)
	}

	for label := range strings.SplitSeq(bare, ".") {
		if err := validateLabel(label, bare); err != nil {
			return err
		}
	}

	return nil
}

func validateLabel(label, name string) error {
	switch {
	case label == "":
		return fmt.Errorf("%q içinde boş etiket var (ardışık nokta)", name)
	case len(label) > maxLabelLength:
		return fmt.Errorf("%q etiketi %d karakter, en fazla %d olabilir", label, len(label), maxLabelLength)
	case strings.HasPrefix(label, "-"), strings.HasSuffix(label, "-"):
		return fmt.Errorf("%q etiketi tire ile başlayamaz veya bitemez", label)
	}

	for _, r := range label {
		isAllowed := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_'

		if !isAllowed {
			return fmt.Errorf("%q etiketi geçersiz karakter içeriyor: %q", label, r)
		}
	}

	return nil
}

// ToASCII converts an internationalised name to punycode so it can be stored
// and compared as ASCII. A name that is already ASCII passes through
// unchanged.
func ToASCII(name string) (string, error) {
	if isASCII(name) {
		return name, nil
	}

	trailing := strings.HasSuffix(name, ".")

	ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(name, "."))
	if err != nil {
		return "", fmt.Errorf("%q punycode'a çevrilemedi: %w", name, err)
	}

	if trailing {
		ascii += "."
	}

	return ascii, nil
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}

	return true
}
