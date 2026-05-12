package entpoly

import "sort"

// polyState is the per-codegen-run state shared between the preprocess
// phase (which walks gen.Graph and strips polymorphic edges) and the
// generate phase (which emits polymorphic.go from the recorded info).
//
// It is rebuilt fresh in every preprocess() call so that running codegen
// multiple times in a single process (e.g. during tests) is deterministic.
type polyState struct {
	// Package is the Go package the sidecar file will live in. Pulled
	// from gen.Config.Package at preprocess time.
	Package string

	// MorphMap is the effective morph map: explicit user-supplied
	// aliases plus auto-derived defaults for every parent participant.
	// Keyed by morph key, valued by ent schema name.
	MorphMap map[string]string

	// Children carries one entry per type declaring MorphTo. Drives
	// the per-child Set/Clear builder method emission and the per-child
	// MorphID/MorphKey method when the child is also a self-referential
	// parent.
	Children []childInfo

	// Parents carries one entry per MorphOne / MorphMany declaration.
	// Reserved for v2 sidecar codegen of typed back-ref methods.
	Parents []parentInfo

	// Holders carries one entry per MorphedByMany declaration. Reserved
	// for v2 sidecar codegen of typed M2M back-ref methods.
	Holders []holderInfo

	// parents is an internal accumulator of every parent participant
	// (host of a MorphOne / MorphMany or Target of a MorphedByMany)
	// used to auto-register morph map entries.
	parents []string
}

// parentNames returns the deduplicated, sorted set of parent participant
// schema names recorded during preprocess. Used to seed default morph map
// entries for types not otherwise registered.
func (s *polyState) parentNames() []string {
	seen := map[string]struct{}{}
	for _, n := range s.parents {
		seen[n] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// childInfo describes one child-side polymorphic declaration. Every field
// is precomputed during preprocess so the codegen template never has to
// do string mutation.
type childInfo struct {
	TypeName     string   // The ent schema name (e.g. "Comment").
	MorphName    string   // The relation name (e.g. "commentable").
	IDColumn     string   // Resolved id column name.
	TypeColumn   string   // Resolved type column name.
	IDType       string   // "string" or "int".
	AllowedTypes []string // Allowed parent schema names from the builder.
}

// parentInfo describes one MorphOne / MorphMany back-reference.
type parentInfo struct {
	ParentName string // The hosting schema name (e.g. "Post").
	FieldName  string // The back-ref method name (e.g. "comments").
	Target     string // The child schema name (e.g. "Comment").
	MorphName  string // The relation name on the child.
	Kind       string // "morphOne" or "morphMany".
	IDColumn   string // Custom id column override (empty for default).
	TypeColumn string // Custom type column override (empty for default).
}

// holderInfo describes one MorphedByMany declaration on the holder side
// (e.g. Tag's "posts" back-ref).
type holderInfo struct {
	HolderName  string // The holder schema name (e.g. "Tag").
	FieldName   string // The back-ref method name (e.g. "posts").
	Target      string // The concrete parent schema name (e.g. "Post").
	Pivot       string // The pivot schema name (e.g. "Taggable").
	ThroughName string // The pivot SQL table name (cosmetic).
	MorphName   string // The relation name on the pivot.
	IDColumn    string // Custom id column override on the pivot.
	TypeColumn  string // Custom type column override on the pivot.
}
