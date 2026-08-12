package records

import "sort"

// Change is the record-level difference between two versions of a file.
//
// The line diff in the unbound package answers "what does the file look like
// now"; this answers "which records moved", which is what the runtime
// unbound-control push needs.
type Change struct {
	Added   []Record
	Removed []Record

	// Retained lists the records that still exist under a removed record's
	// name. unbound-control local_data_remove deletes every type for a name at
	// once, so those have to be pushed back or the daemon would lose records
	// the file still holds.
	Retained []Record

	// ZonesRemoved lists the local-zone declarations that disappeared, so the
	// daemon can drop them too.
	ZonesRemoved []string
}

// Empty reports whether nothing moved.
func (c Change) Empty() bool {
	return len(c.Added) == 0 && len(c.Removed) == 0 && len(c.ZonesRemoved) == 0
}

// Diff computes what changed between two versions of a file.
//
// A record whose value changed appears in both Added and Removed, which is
// correct for the runtime push: the old data is dropped and the new data is
// installed.
func Diff(before, after *File) Change {
	var change Change

	beforeRecords := indexByFullKey(before)
	afterRecords := indexByFullKey(after)

	for key, rec := range afterRecords {
		if _, existed := beforeRecords[key]; !existed {
			change.Added = append(change.Added, rec)
		}
	}

	for key, rec := range beforeRecords {
		if _, survives := afterRecords[key]; !survives {
			change.Removed = append(change.Removed, rec)
		}
	}

	change.Retained = retainedFor(change.Removed, after)
	change.ZonesRemoved = removedZones(before, after)

	sortRecords(change.Added)
	sortRecords(change.Removed)
	sortRecords(change.Retained)
	sort.Strings(change.ZonesRemoved)

	return change
}

func indexByFullKey(f *File) map[string]Record {
	if f == nil {
		return nil
	}

	out := make(map[string]Record)
	for _, rec := range f.All() {
		out[rec.FullKey()] = rec
	}

	return out
}

// retainedFor collects every record the file still holds under a name that a
// removal will wipe.
func retainedFor(removed []Record, after *File) []Record {
	if len(removed) == 0 || after == nil {
		return nil
	}

	names := make(map[string]bool, len(removed))
	for _, rec := range removed {
		names[rec.Name] = true
	}

	var out []Record

	for _, rec := range after.All() {
		if names[rec.Name] {
			out = append(out, rec)
		}
	}

	return out
}

func removedZones(before, after *File) []string {
	if before == nil {
		return nil
	}

	var (
		surviving map[string]string
		out       []string
	)

	if after != nil {
		surviving = after.Zones()
	}

	for zone := range before.Zones() {
		if _, kept := surviving[zone]; !kept {
			out = append(out, zone)
		}
	}

	return out
}

func sortRecords(recs []Record) {
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Name != recs[j].Name {
			return recs[i].Name < recs[j].Name
		}
		if recs[i].Type != recs[j].Type {
			return recs[i].Type < recs[j].Type
		}

		return recs[i].Value < recs[j].Value
	})
}
