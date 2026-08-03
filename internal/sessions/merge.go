package sessions

import (
	"path/filepath"
	"sort"
	"strings"
)

// Merge folds a session's parts into its primary digest.
//
// A subagent's reads and edits are part of the work the dispatching session
// did: attributing them to a separate node would fragment one piece of work
// across dozens of entries, and leaving them out makes the parent under-report
// what it touched. So paths union, tool calls sum, and intents concatenate.
//
// Four things deliberately do not merge:
//
//   - Identity (id, title, cwd, source) stays the primary's. A part carries its
//     own session header, and letting it win would re-attribute the session to
//     whatever directory the subagent happened to run in.
//   - UserTurns stays the primary's. A part's `user` messages are the
//     orchestrator's prompt, not a person's turn; counting them would inflate
//     the one number that means "how often did a human intervene". The part
//     count is reported separately as Subagents.
//   - The task list stays the primary's. A subagent tracks its own breakdown of
//     one delegated slice; splicing those into the dispatching session's list
//     would bury the plan a person actually wrote under machine-generated
//     fragments. (In practice OMP subagents do not use the tracker at all.)
//   - Offsets and sizes are per file and stay on the part digests, which the
//     store keeps so an appended part re-parses from where it left off.
func Merge(primary Digest, parts []Digest) Digest {
	merged := *primary.clone()
	if len(parts) == 0 {
		return merged
	}
	if merged.ToolCalls == nil {
		merged.ToolCalls = map[string]int{}
	}

	ordered := append([]Digest(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].StartedAt.Equal(ordered[j].StartedAt) {
			return ordered[i].StartedAt.Before(ordered[j].StartedAt)
		}
		return ordered[i].Path < ordered[j].Path
	})

	for _, part := range ordered {
		merged.Subagents = appendUnique(merged.Subagents, subagentName(part.Path))
		for name, count := range part.ToolCalls {
			merged.ToolCalls[name] += count
		}
		for _, path := range part.Writes {
			merged.Writes = appendUnique(merged.Writes, path)
		}
		for day, count := range part.Activity {
			// A subagent's work happened on a real day and is this session's
			// work, so it counts toward that day exactly like its tool calls do.
			if merged.Activity == nil {
				merged.Activity = map[string]int{}
			}
			merged.Activity[day] += count
		}
		for _, path := range part.Edits {
			merged.Edits = appendUnique(merged.Edits, path)
		}
		for _, path := range part.Reads {
			merged.Reads = appendUnique(merged.Reads, path)
		}
		for _, intent := range part.Intents {
			if len(merged.Intents) > 0 && merged.Intents[len(merged.Intents)-1] == intent {
				continue
			}
			merged.Intents = append(merged.Intents, intent)
		}
		merged.IntentsTruncated = merged.IntentsTruncated || part.IntentsTruncated
		merged.PathsTruncated = merged.PathsTruncated || part.PathsTruncated
		// A subagent can outlive the last event on the primary transcript, and
		// can have started before it: the session's span covers all its work.
		if !part.StartedAt.IsZero() && (merged.StartedAt.IsZero() || part.StartedAt.Before(merged.StartedAt)) {
			merged.StartedAt = part.StartedAt
		}
		if part.UpdatedAt.After(merged.UpdatedAt) {
			merged.UpdatedAt = part.UpdatedAt
		}
	}

	if len(merged.ToolCalls) == 0 {
		merged.ToolCalls = nil
	}
	sort.Strings(merged.Writes)
	sort.Strings(merged.Edits)
	sort.Strings(merged.Reads)
	sort.Strings(merged.Subagents)
	merged.capPayload()
	return merged
}

// capPayload re-applies the payload caps after merging, so a session with many
// subagents cannot grow an unbounded response. Intents keep their most recent
// entries for the same reason the parser does; paths are a set, so the tail goes.
func (d *Digest) capPayload() {
	if len(d.Intents) > MaxIntents {
		d.Intents = d.Intents[len(d.Intents)-MaxIntents:]
		d.IntentsTruncated = true
	}
	d.trimIntentChars()
	for _, list := range []*[]string{&d.Writes, &d.Edits, &d.Reads} {
		if len(*list) > MaxPaths {
			*list = (*list)[:MaxPaths]
			d.PathsTruncated = true
		}
	}
}

// subagentName recovers the name a part was dispatched under, which OMP uses as
// the transcript's file name.
func subagentName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

func appendUnique(list []string, value string) []string {
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
