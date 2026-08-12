// Package records models the local-zone and local-data lines of an Unbound
// records file, and parses and writes that file without disturbing the parts
// it does not manage.
package records

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
)

// Type is a DNS resource record type.
type Type string

const (
	TypeA     Type = "A"
	TypeAAAA  Type = "AAAA"
	TypeCNAME Type = "CNAME"
	TypeTXT   Type = "TXT"
	TypeMX    Type = "MX"
	TypePTR   Type = "PTR"
)

// SupportedTypes lists the record types this tool writes, in the order they
// are offered to the user.
var SupportedTypes = []Type{TypeA, TypeAAAA, TypeCNAME, TypeTXT, TypeMX, TypePTR}

// ParseType accepts a type name in any case and reports whether it is
// supported.
func ParseType(s string) (Type, error) {
	upper := Type(strings.ToUpper(strings.TrimSpace(s)))

	for _, t := range SupportedTypes {
		if t == upper {
			return t, nil
		}
	}

	names := make([]string, len(SupportedTypes))
	for i, t := range SupportedTypes {
		names[i] = string(t)
	}

	return "", fmt.Errorf("desteklenmeyen kayıt tipi %q (desteklenenler: %s)", s, strings.Join(names, ", "))
}

// DefaultClass is the only class this tool writes. Unbound accepts others but
// nothing here needs them.
const DefaultClass = "IN"

// Record is a single local-data entry.
type Record struct {
	// Name always carries a trailing dot, which is how Unbound stores it.
	Name string

	// TTL is nil when the record does not carry one, in which case Unbound
	// applies its own default.
	TTL *uint32

	Class string
	Type  Type
	Value string
}

// Key identifies a record for comparison and de-duplication. The value is
// deliberately excluded so a name/type pair with a different value counts as
// the same record, which is what makes a conflict detectable.
func (r Record) Key() string {
	return strings.ToLower(r.Name) + "|" + string(r.Type)
}

// FullKey includes the value, for diffing where a changed value matters.
func (r Record) FullKey() string {
	return r.Key() + "|" + r.Value
}

// Equal reports whether two records are identical in every field that reaches
// the file. TTL is a pointer, so the struct cannot simply be compared.
//
// This is what tells a write it has nothing to do. Rewriting a line that
// already says the right thing would re-render it from the record and lose the
// operator's own formatting.
func (r Record) Equal(other Record) bool {
	if r.Key() != other.Key() || r.Value != other.Value {
		return false
	}

	if r.class() != other.class() {
		return false
	}

	switch {
	case r.TTL == nil && other.TTL == nil:
		return true
	case r.TTL == nil || other.TTL == nil:
		return false
	default:
		return *r.TTL == *other.TTL
	}
}

// class returns the effective class, since an empty one means the default.
func (r Record) class() string {
	if r.Class == "" {
		return DefaultClass
	}

	return r.Class
}

// String renders the record as it appears inside the quotes of a local-data
// line.
func (r Record) String() string {
	parts := make([]string, 0, 5)
	parts = append(parts, r.Name)

	if r.TTL != nil {
		parts = append(parts, strconv.FormatUint(uint64(*r.TTL), 10))
	}

	parts = append(parts, r.class(), string(r.Type), r.rdata())

	return strings.Join(parts, " ")
}

// rdata renders the value as Unbound expects it on the wire format line.
func (r Record) rdata() string {
	// TXT data is a character-string and must be quoted, otherwise a value
	// with spaces is read as several separate strings.
	if r.Type == TypeTXT && !isQuoted(r.Value) {
		return `"` + strings.ReplaceAll(r.Value, `"`, `\"`) + `"`
	}

	return r.Value
}

func isQuoted(s string) bool {
	return len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"'
}

// Zone returns the zone a local-zone entry should declare for this record.
func (r Record) Zone() string {
	return ZoneFor(r.Name)
}

// ZoneFor picks the zone to declare for a record name.
//
// Normally that is the parent, so "mail.google.com." yields "google.com." and
// the rest of google.com keeps resolving normally. When the parent would be a
// bare TLD the name itself is used instead: declaring "com." transparent puts
// an entry for an entire top-level domain into the config, which is far wider
// than anything the user asked for.
func ZoneFor(name string) string {
	name = NormalizeName(name)

	parent := ParentZone(name)
	if labelCount(parent) < 2 {
		return name
	}

	return parent
}

// ParentZone strips the first label from a name. "mail.google.com." becomes
// "google.com.".
func ParentZone(name string) string {
	name = NormalizeName(name)

	_, rest, found := strings.Cut(name, ".")
	if !found || rest == "" || rest == "." {
		return name
	}

	return rest
}

// labelCount counts the labels in a name, ignoring the root dot.
func labelCount(name string) int {
	bare := strings.TrimSuffix(NormalizeName(name), ".")
	if bare == "" {
		return 0
	}

	return strings.Count(bare, ".") + 1
}

// NormalizeName lower-cases a name and gives it the trailing dot Unbound
// expects, so two spellings of one name compare equal.
func NormalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	if !strings.HasSuffix(name, ".") {
		name += "."
	}

	return name
}

// New builds a validated record from user input.
func New(name string, t Type, value string, ttl *uint32) (Record, error) {
	r := Record{
		Name:  NormalizeName(name),
		TTL:   ttl,
		Class: DefaultClass,
		Type:  t,
		Value: strings.TrimSpace(value),
	}

	if err := r.Validate(); err != nil {
		return Record{}, err
	}

	return r, nil
}

// Validate checks the name and the value against the rules for the record
// type.
func (r Record) Validate() error {
	if err := ValidateName(r.Name); err != nil {
		return err
	}

	if r.Value == "" {
		return fmt.Errorf("%s kaydının değeri boş olamaz", r.Type)
	}

	switch r.Type {
	case TypeA:
		return validateIPv4(r.Value)
	case TypeAAAA:
		return validateIPv6(r.Value)
	case TypeCNAME:
		return validateCNAME(r)
	case TypeTXT:
		return validateTXT(r.Value)
	case TypeMX:
		return validateMX(r.Value)
	case TypePTR:
		return validatePTR(r)
	default:
		return fmt.Errorf("desteklenmeyen kayıt tipi %q", r.Type)
	}
}

func validateIPv4(value string) error {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("%q geçerli bir IPv4 adresi değil", value)
	}

	return nil
}

func validateIPv6(value string) error {
	ip := net.ParseIP(value)
	if ip == nil {
		return fmt.Errorf("%q geçerli bir IPv6 adresi değil", value)
	}

	// To4 returning non-nil means the text was an IPv4 address, which belongs
	// in an A record.
	if ip.To4() != nil {
		return fmt.Errorf("%q bir IPv4 adresi, AAAA kaydı IPv6 bekler", value)
	}

	return nil
}

func validateCNAME(r Record) error {
	if err := ValidateName(r.Value); err != nil {
		return fmt.Errorf("CNAME hedefi geçersiz: %w", err)
	}

	if NormalizeName(r.Value) == NormalizeName(r.Name) {
		return fmt.Errorf("CNAME kendini gösteremez (%s)", r.Name)
	}

	return nil
}

// maxTXTChunk is the largest single character-string a TXT record may hold.
const maxTXTChunk = 255

func validateTXT(value string) error {
	// Unbound stores TXT data as one or more character-strings; a single
	// string longer than 255 bytes has to be split by the caller.
	if len(value) > maxTXTChunk {
		return fmt.Errorf("TXT değeri %d bayt, tek parça en fazla %d bayt olabilir", len(value), maxTXTChunk)
	}

	return nil
}

func validateMX(value string) error {
	priority, host, found := strings.Cut(strings.TrimSpace(value), " ")
	if !found {
		return fmt.Errorf("MX değeri %q, \"<öncelik> <hedef>\" biçiminde olmalı (örnek: 10 mail.example.com)", value)
	}

	if _, err := strconv.ParseUint(strings.TrimSpace(priority), 10, 16); err != nil {
		return fmt.Errorf("MX önceliği %q geçersiz, 0-65535 arası bir sayı olmalı", priority)
	}

	if err := ValidateName(strings.TrimSpace(host)); err != nil {
		return fmt.Errorf("MX hedefi geçersiz: %w", err)
	}

	return nil
}

func validatePTR(r Record) error {
	name := NormalizeName(r.Name)

	if !strings.HasSuffix(name, ".in-addr.arpa.") && !strings.HasSuffix(name, ".ip6.arpa.") {
		return fmt.Errorf(
			"PTR kaydının adı .in-addr.arpa. veya .ip6.arpa. ile bitmeli, %q verilmiş (IP için: %s)",
			r.Name, suggestReverseName(r.Name))
	}

	if err := ValidateName(r.Value); err != nil {
		return fmt.Errorf("PTR hedefi geçersiz: %w", err)
	}

	return nil
}

// suggestReverseName turns an IPv4 address into its in-addr.arpa name, so the
// error message can show the user what they probably meant.
func suggestReverseName(value string) string {
	ip := net.ParseIP(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if ip == nil {
		return "örnek 10.10.10.10.in-addr.arpa."
	}

	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", v4[3], v4[2], v4[1], v4[0])
	}

	return "örnek 10.10.10.10.in-addr.arpa."
}

// ReverseName converts an IP address to the name a PTR record must use.
func ReverseName(ip string) (string, error) {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return "", fmt.Errorf("%q geçerli bir IP adresi değil", ip)
	}

	if v4 := parsed.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", v4[3], v4[2], v4[1], v4[0]), nil
	}

	// IPv6: every nibble becomes a label, least significant first.
	var b strings.Builder
	for _, octet := range slices.Backward(parsed) {
		fmt.Fprintf(&b, "%x.%x.", octet&0x0f, octet>>4)
	}
	b.WriteString("ip6.arpa.")

	return b.String(), nil
}
