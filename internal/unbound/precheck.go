package unbound

import (
	"context"
	"fmt"
	"strings"

	"github.com/KilimcininKorOglu/yada/internal/config"
	"github.com/KilimcininKorOglu/yada/internal/records"
	"github.com/KilimcininKorOglu/yada/internal/transport"
)

// AddOutcome says what an add would run into, so the caller can ask before
// writing rather than report a refusal afterwards.
type AddOutcome int

const (
	// AddWritable means nothing holds the name and type with a different
	// value. The write may proceed: it adds where the record is missing and
	// corrects a differing TTL where it is not.
	AddWritable AddOutcome = iota

	// AddDuplicate means every readable server already holds exactly this
	// record. There is nothing to write.
	AddDuplicate

	// AddConflict means at least one readable server holds the name and type
	// with a different value. Writing would replace what is there, which is a
	// decision for the user rather than for the tool.
	AddConflict
)

// ServerState is what one server holds for the name and type being added.
type ServerState struct {
	Server config.Server

	// Existing is nil when the server does not hold the name and type.
	Existing *records.Record

	// Matches is true when Existing already carries the value being added, so
	// the summary can say which servers need nothing.
	Matches bool

	// Err is set when the server's records file could not be read.
	Err error
}

// AddCheck is the result of looking at every server before a write.
type AddCheck struct {
	Outcome AddOutcome

	// States keeps the servers in configuration order, so the summary reads
	// the same way as the rest of the output.
	States []ServerState

	Readable   int
	Unreadable int
}

// CheckAdd reads every server and reports what an add would run into.
//
// This is a second read: the write performs its own, so a record can appear
// between the two. That window is harmless, because the write uses Set, which
// replaces whatever it finds instead of refusing.
func CheckAdd(ctx context.Context, r transport.Runner, cfg config.Config, rec records.Record) AddCheck {
	return classifyAdd(rec, ReadAll(ctx, r, cfg))
}

// classifyAdd holds the decision itself, separate from the reading, so the
// rules can be exercised without a server.
func classifyAdd(rec records.Record, results []ServerRecords) AddCheck {
	check := AddCheck{States: make([]ServerState, 0, len(results))}

	conflict := false
	// A duplicate has to be true of every readable server, so it starts out
	// true and any server that disagrees clears it.
	duplicate := true

	for _, res := range results {
		state := ServerState{Server: res.Server, Err: res.Err}

		if res.Err != nil || res.File == nil {
			check.Unreadable++
			check.States = append(check.States, state)

			continue
		}

		check.Readable++

		found := res.File.Find(rec.Name, rec.Type)
		if len(found) == 0 {
			duplicate = false
			check.States = append(check.States, state)

			continue
		}

		existing := found[0]
		state.Existing = &existing
		state.Matches = existing.Value == rec.Value

		switch {
		case !state.Matches:
			conflict = true
			duplicate = false
		case !existing.Equal(rec):
			// Same value, different TTL. Worth writing, but not worth asking
			// about: nothing the user typed is being replaced.
			duplicate = false
		}

		check.States = append(check.States, state)
	}

	switch {
	case conflict:
		check.Outcome = AddConflict
	case check.Readable > 0 && duplicate:
		check.Outcome = AddDuplicate
	default:
		check.Outcome = AddWritable
	}

	return check
}

// Summary renders one line per server, which is what both interfaces show when
// they ask the user to decide.
func (c AddCheck) Summary() string {
	var b strings.Builder

	for _, state := range c.States {
		label := state.Server.Label()

		switch {
		case state.Err != nil:
			fmt.Fprintf(&b, "%s: okunamadı (%v)\n", label, state.Err)
		case state.Existing == nil:
			fmt.Fprintf(&b, "%s: kayıt yok\n", label)
		case state.Matches:
			fmt.Fprintf(&b, "%s: %s (aynı)\n", label, state.Existing.Value)
		default:
			fmt.Fprintf(&b, "%s: %s\n", label, state.Existing.Value)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
