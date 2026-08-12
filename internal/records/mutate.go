package records

import (
	"fmt"
	"strings"
)

// DefaultZoneType is what a generated local-zone entry uses. transparent lets
// Unbound fall through to the real resolver for names the zone does not
// define, which is what makes overriding a single host safe.
const DefaultZoneType = "transparent"

// All returns every record in the file, in file order.
func (f *File) All() []Record {
	out := make([]Record, 0, len(f.Lines))

	for _, line := range f.Lines {
		if line.Kind == KindData {
			out = append(out, line.Record)
		}
	}

	return out
}

// Zones returns every zone declared in the file, keyed by normalized name.
func (f *File) Zones() map[string]string {
	zones := make(map[string]string)

	for _, line := range f.Lines {
		if line.Kind == KindZone {
			zones[line.ZoneName] = line.ZoneType
		}
	}

	return zones
}

// Find returns the records matching a name, and optionally a type. An empty
// type matches every type.
func (f *File) Find(name string, t Type) []Record {
	target := NormalizeName(name)

	var out []Record

	for _, line := range f.Lines {
		if line.Kind != KindData {
			continue
		}

		if line.Record.Name != target {
			continue
		}

		if t != "" && line.Record.Type != t {
			continue
		}

		out = append(out, line.Record)
	}

	return out
}

// ErrExists reports an attempt to add a record whose name and type are already
// present.
type ErrExists struct {
	Existing Record
}

func (e *ErrExists) Error() string {
	return fmt.Sprintf("%s için %s kaydı zaten var: %s",
		strings.TrimSuffix(e.Existing.Name, "."), e.Existing.Type, e.Existing.Value)
}

// Add appends a record and, when needed, the local-zone line its parent zone
// requires. It refuses to add a second record for a name and type that already
// exist, because two answers for one name is a misconfiguration rather than a
// useful state.
func (f *File) Add(rec Record) error {
	if err := rec.Validate(); err != nil {
		return err
	}

	for _, line := range f.Lines {
		if line.Kind == KindData && line.Record.Key() == rec.Key() {
			return &ErrExists{Existing: line.Record}
		}
	}

	f.ensureZone(rec.Zone())

	f.Lines = append(f.Lines, Line{
		Kind:      KindData,
		Record:    rec,
		generated: true,
	})

	return nil
}

// ensureZone appends a local-zone line unless the zone is already declared.
func (f *File) ensureZone(zone string) {
	if zone == "" || zone == "." {
		return
	}

	if _, exists := f.Zones()[zone]; exists {
		return
	}

	f.Lines = append(f.Lines, Line{
		Kind:      KindZone,
		ZoneName:  zone,
		ZoneType:  DefaultZoneType,
		generated: true,
	})
}

// Delete removes records matching the name and, when given, the type and
// value. It returns how many were removed.
func (f *File) Delete(name string, t Type, value string) int {
	target := NormalizeName(name)

	kept := make([]Line, 0, len(f.Lines))
	removed := 0

	for _, line := range f.Lines {
		if line.Kind == KindData && matches(line.Record, target, t, value) {
			removed++
			continue
		}

		kept = append(kept, line)
	}

	f.Lines = kept

	return removed
}

func matches(rec Record, name string, t Type, value string) bool {
	if rec.Name != name {
		return false
	}

	if t != "" && rec.Type != t {
		return false
	}

	if value != "" && rec.Value != value {
		return false
	}

	return true
}

// Update replaces the value of an existing record. Editing in place keeps the
// record's position in the file, so a change does not reorder the output.
func (f *File) Update(rec Record) error {
	if err := rec.Validate(); err != nil {
		return err
	}

	for i, line := range f.Lines {
		if line.Kind != KindData || line.Record.Key() != rec.Key() {
			continue
		}

		f.Lines[i].Record = rec
		f.Lines[i].generated = true

		return nil
	}

	return fmt.Errorf("%s için %s kaydı bulunamadı", strings.TrimSuffix(rec.Name, "."), rec.Type)
}

// PruneUnusedZones removes transparent zone lines that no record needs any
// more.
//
// Only transparent zones are touched. A transparent zone with no local-data
// does nothing, since transparent means "fall through to the real resolver",
// which is already the default. Other zone types are left alone: a static,
// refuse or redirect zone with no records is a deliberate policy, and deleting
// it would change how the resolver answers.
//
// Whether a line was written by this tool cannot be used as the criterion,
// because that information is lost as soon as the file is read back from the
// server.
func (f *File) PruneUnusedZones() int {
	needed := make(map[string]bool)

	for _, line := range f.Lines {
		if line.Kind == KindData {
			needed[line.Record.Zone()] = true
		}
	}

	kept := make([]Line, 0, len(f.Lines))
	removed := 0

	for _, line := range f.Lines {
		unused := line.Kind == KindZone &&
			line.ZoneType == DefaultZoneType &&
			!needed[line.ZoneName]

		if unused {
			removed++
			continue
		}

		kept = append(kept, line)
	}

	f.Lines = kept

	return removed
}
