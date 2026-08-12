// Package diff compares the record sets of several servers.
package diff

import (
	"sort"

	"github.com/kerem/unbound-dns/internal/records"
)

// Status classifies one record across the compared servers.
type Status int

const (
	// StatusSame means every server has the record with the same value.
	StatusSame Status = iota

	// StatusMissing means at least one server does not have the record.
	StatusMissing

	// StatusConflict means the servers hold different values for the same
	// name and type. This is never resolved automatically: picking a winner
	// would silently discard whichever value the user actually wanted.
	StatusConflict
)

func (s Status) String() string {
	switch s {
	case StatusSame:
		return "aynı"
	case StatusMissing:
		return "eksik"
	case StatusConflict:
		return "çakışma"
	default:
		return "bilinmiyor"
	}
}

// Entry is one record and where it stands across the servers.
type Entry struct {
	Record records.Record
	Status Status

	// Values maps a server label to the value it holds. A server absent from
	// this map does not have the record at all.
	Values map[string]string

	// Present lists the servers holding the record, and Missing those that do
	// not. Both are sorted for stable output.
	Present []string
	Missing []string
}

// ServerSet pairs a server label with its records.
type ServerSet struct {
	Label   string
	Records []records.Record
}

// Result is the whole comparison.
type Result struct {
	Servers []string
	Entries []Entry
}

// Conflicts returns only the entries whose values disagree.
func (r Result) Conflicts() []Entry {
	return r.filter(StatusConflict)
}

// Missing returns only the entries absent from at least one server.
func (r Result) Missing() []Entry {
	return r.filter(StatusMissing)
}

func (r Result) filter(status Status) []Entry {
	var out []Entry

	for _, e := range r.Entries {
		if e.Status == status {
			out = append(out, e)
		}
	}

	return out
}

// InSync reports whether every server holds the same records with the same
// values.
func (r Result) InSync() bool {
	for _, e := range r.Entries {
		if e.Status != StatusSame {
			return false
		}
	}

	return true
}

// Compare builds the comparison across the given servers.
//
// Records are matched by name and type; TTL is deliberately excluded, because
// a differing TTL is not a difference in what the resolver answers.
func Compare(sets []ServerSet) Result {
	labels := make([]string, 0, len(sets))
	byKey := make(map[string]*Entry)
	var order []string

	for _, set := range sets {
		labels = append(labels, set.Label)

		for _, rec := range set.Records {
			key := rec.Key()

			entry, seen := byKey[key]
			if !seen {
				entry = &Entry{Record: rec, Values: map[string]string{}}
				byKey[key] = entry
				order = append(order, key)
			}

			entry.Values[set.Label] = rec.Value
		}
	}

	sort.Strings(order)

	result := Result{Servers: labels, Entries: make([]Entry, 0, len(order))}

	for _, key := range order {
		entry := byKey[key]
		classify(entry, labels)
		result.Entries = append(result.Entries, *entry)
	}

	return result
}

// classify fills in the presence lists and the status of one entry.
func classify(entry *Entry, labels []string) {
	distinct := make(map[string]bool)

	for _, label := range labels {
		value, ok := entry.Values[label]
		if !ok {
			entry.Missing = append(entry.Missing, label)
			continue
		}

		entry.Present = append(entry.Present, label)
		distinct[value] = true
	}

	sort.Strings(entry.Present)
	sort.Strings(entry.Missing)

	switch {
	case len(distinct) > 1:
		// A value mismatch outranks a missing copy: syncing a record the
		// servers disagree about would spread one of two answers without
		// anyone having chosen it.
		entry.Status = StatusConflict
	case len(entry.Missing) > 0:
		entry.Status = StatusMissing
	default:
		entry.Status = StatusSame
	}
}

// Plan is what sync would do to one server.
type Plan struct {
	Server string

	// Add lists records the server lacks.
	Add []records.Record

	// Remove lists records the server has that the source does not. It is
	// only filled when pruning is requested.
	Remove []records.Record
}

// Empty reports whether the plan changes nothing.
func (p Plan) Empty() bool {
	return len(p.Add) == 0 && len(p.Remove) == 0
}

// PlanSync works out how to make the targets match the source.
//
// Conflicting records are skipped and returned separately: the caller has to
// surface them, because choosing a value is the user's decision.
func PlanSync(source ServerSet, targets []ServerSet, prune bool) ([]Plan, []Entry) {
	comparison := Compare(append([]ServerSet{source}, targets...))

	conflicts := comparison.Conflicts()
	conflicted := make(map[string]bool, len(conflicts))

	for _, entry := range conflicts {
		conflicted[entry.Record.Key()] = true
	}

	sourceByKey := make(map[string]records.Record, len(source.Records))
	for _, rec := range source.Records {
		sourceByKey[rec.Key()] = rec
	}

	plans := make([]Plan, 0, len(targets))

	for _, target := range targets {
		plan := Plan{Server: target.Label}

		targetByKey := make(map[string]records.Record, len(target.Records))
		for _, rec := range target.Records {
			targetByKey[rec.Key()] = rec
		}

		for _, rec := range source.Records {
			if conflicted[rec.Key()] {
				continue
			}

			if _, exists := targetByKey[rec.Key()]; !exists {
				plan.Add = append(plan.Add, rec)
			}
		}

		if prune {
			for _, rec := range target.Records {
				if conflicted[rec.Key()] {
					continue
				}

				if _, exists := sourceByKey[rec.Key()]; !exists {
					plan.Remove = append(plan.Remove, rec)
				}
			}
		}

		plans = append(plans, plan)
	}

	return plans, conflicts
}
