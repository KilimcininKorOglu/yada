package records

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// indent is the leading whitespace written on generated lines. It matches what
// the PowerShell script produced, so an existing file gains no spurious diff.
const indent = "         "

// LineKind classifies a line of the records file.
type LineKind int

const (
	// KindOther covers comments, blank lines and any directive this tool does
	// not manage. Such lines are preserved byte for byte.
	KindOther LineKind = iota
	KindZone
	KindData
)

// Line is one line of the file. Raw always holds the original text; it is what
// gets written back for lines this tool did not create.
type Line struct {
	Raw  string
	Kind LineKind

	// Record is set when Kind is KindData.
	Record Record

	// ZoneName and ZoneType are set when Kind is KindZone.
	ZoneName string
	ZoneType string

	// generated marks lines this tool produced, which are re-rendered from
	// their fields rather than copied from Raw.
	generated bool
}

// File is a parsed records file. The line order is preserved so writing it
// back leaves untouched content exactly as it was found.
type File struct {
	Lines []Line

	// trailingNewline records whether the source ended with a newline, so the
	// round trip does not add or drop one.
	trailingNewline bool
}

// Parse reads a records file. Lines that are not local-zone or local-data
// entries are kept verbatim, which is what stops a rewrite from discarding the
// operator's own comments.
func Parse(data []byte) (*File, error) {
	f := &File{trailingNewline: bytes.HasSuffix(data, []byte("\n"))}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Records files are small, but a pathological line should not panic the
	// scanner with the default 64 KiB limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		f.Lines = append(f.Lines, parseLine(scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("kayıt dosyası okunamadı: %w", err)
	}

	return f, nil
}

func parseLine(raw string) Line {
	line := Line{Raw: raw, Kind: KindOther}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return line
	}

	directive, rest, found := strings.Cut(trimmed, ":")
	if !found {
		return line
	}

	body := strings.TrimSpace(rest)

	switch strings.TrimSpace(directive) {
	case "local-zone":
		name, zoneType, ok := parseZoneBody(body)
		if !ok {
			return line
		}

		line.Kind = KindZone
		line.ZoneName = name
		line.ZoneType = zoneType

	case "local-data":
		rec, ok := parseDataBody(body)
		if !ok {
			return line
		}

		line.Kind = KindData
		line.Record = rec
	}

	return line
}

// parseZoneBody reads `"google.com." transparent`, tolerating missing quotes.
func parseZoneBody(body string) (name, zoneType string, ok bool) {
	value, rest, ok := cutQuoted(body)
	if !ok {
		fields := strings.Fields(body)
		if len(fields) < 2 {
			return "", "", false
		}

		return NormalizeName(fields[0]), fields[1], true
	}

	zoneType = strings.TrimSpace(rest)
	if zoneType == "" {
		return "", "", false
	}

	return NormalizeName(value), zoneType, true
}

// parseDataBody reads `"mail.google.com. IN A 10.10.10.10"` into a record.
func parseDataBody(body string) (Record, bool) {
	value, _, ok := cutQuoted(body)
	if !ok {
		// An unquoted body is unusual but still parseable.
		value = body
	}

	fields := strings.Fields(value)
	if len(fields) < 3 {
		return Record{}, false
	}

	rec := Record{Name: NormalizeName(fields[0]), Class: DefaultClass}
	rest := fields[1:]

	// An optional TTL comes before the class.
	if ttl, err := strconv.ParseUint(rest[0], 10, 32); err == nil {
		rec.TTL = SetTTL(uint32(ttl))
		rest = rest[1:]
	}

	if len(rest) < 2 {
		return Record{}, false
	}

	// The class is optional too; when present it precedes the type.
	switch strings.ToUpper(rest[0]) {
	case "IN", "CH", "HS":
		rec.Class = strings.ToUpper(rest[0])
		rest = rest[1:]
	}

	if len(rest) < 2 {
		return Record{}, false
	}

	rec.Type = Type(strings.ToUpper(rest[0]))
	rec.Value = strings.Join(rest[1:], " ")

	return rec, true
}

// cutQuoted extracts a leading quoted string, accepting either quote
// character. Unbound allows single quotes so a value containing double quotes,
// such as TXT data, can be written without escaping.
func cutQuoted(s string) (value, rest string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}

	quote := s[0]
	if quote != '"' && quote != '\'' {
		return "", "", false
	}

	end := strings.IndexByte(s[1:], quote)
	if end < 0 {
		return "", "", false
	}

	return s[1 : 1+end], s[2+end:], true
}

// Bytes renders the file. Lines that came from the source and were not
// modified are written from Raw, so their formatting survives untouched.
func (f *File) Bytes() []byte {
	var b strings.Builder

	for i, line := range f.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(line.render())
	}

	if f.trailingNewline && len(f.Lines) > 0 {
		b.WriteByte('\n')
	}

	return []byte(b.String())
}

func (l Line) render() string {
	if !l.generated {
		return l.Raw
	}

	switch l.Kind {
	case KindZone:
		return fmt.Sprintf("%slocal-zone: %s %s", indent, quote(l.ZoneName), l.ZoneType)
	case KindData:
		return fmt.Sprintf("%slocal-data: %s", indent, quote(l.Record.String()))
	default:
		return l.Raw
	}
}

// quote wraps a value for the config file, switching to single quotes when the
// value itself contains a double quote, which TXT data commonly does.
func quote(value string) string {
	if strings.Contains(value, `"`) {
		return "'" + value + "'"
	}

	return `"` + value + `"`
}
