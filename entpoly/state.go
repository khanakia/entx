// state.go — defines the polyState struct that bridges the two halves of
// the codegen pipeline. preprocess.go writes to it; generate.go reads from
// it. Nothing in this file performs IO or graph mutation; it is pure
// data-shape definition.
//
// Notes:
//
//   - polyState is rebuilt fresh on every preprocess() call (NOT cached
//     across runs). This is what makes the tests deterministic and what
//     lets us run codegen multiple times in a single process without
//     leaking state — important because the test suite does exactly
//     that.
//
//   - childInfo / parentInfo / holderInfo are the three "kinds" of
//     polymorphic declaration the extension recognises. If you add a
//     new kind (e.g. MorphOneOfMany for Laravel's $latestOfMany), add
//     a corresponding info struct here, populate it from preprocess,
//     and consume it from buildTmplData.
//
//   - resolveTargetRef carries the parent's GO ID type ("int", "int64",
//     "string", "uuid.UUID", ...). This is populated by preprocess by
//     looking up the target in gen.Graph; the template uses it to pick
//     the right strconv flavour in the forward resolver. When adding
//     support for new ID types (e.g. UUIDs from field.UUID), update the
//     template's switch in the resolver section to handle the new
//     IDGoType string.
//
//   - The unexported `parents` field on polyState is the accumulator
//     for auto-registering snake_case morph keys. Every type seen as a
//     parent in any MorphTo/MorphMany/MorphOne/MorphedByMany lands here;
//     preprocess derives default morph map entries from this set at
//     the tail of the pass.
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

	// pivotMorph maps a type name → the MorphName declared by its
	// MorphTo edge (if any). Populated in a pre-pass at preprocess
	// start so handleHolder can resolve a MorphedByMany's morph name
	// from the pivot's declaration instead of singularise(table),
	// without depending on edge iteration order.
	pivotMorph map[string]string
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
	TypeName       string             // The ent schema name (e.g. "Comment").
	MorphName      string             // The relation name (e.g. "commentable").
	IDColumn       string             // Resolved id column name.
	TypeColumn     string             // Resolved type column name.
	TypeIsEnum     bool               // True when the type column was emitted as a
	// field.Enum (via MixinAllowed). When true, ent
	// generates a named string type (<pkg>.<TypeField>)
	// so the template can use it as a type conversion.
	// When false, the column is a plain string and the
	// identifier <pkg>.<TypeField> resolves to the
	// predicate-EQ shortcut function instead — see
	// docs/entpoly-gql-bug background and template
	// branches on TypeIsEnum.
	IDType         string             // "string" or "int" — the morph_id column type.
	AllowedTypes   []string           // Allowed parent schema names from the builder.
	Required       bool               // True when MorphTo(...).Required() was set — the
	// emitted runtime hook rejects Saves that leave
	// the discriminator pair unset or clear it.
	Touch          bool               // True when MorphTo(...).Touch() was set — Laravel
	// $touches semantics; on every child Save the
	// parent's TouchField is bumped to time.Now().
	TouchField     string             // Parent column name to bump (default "updated_at").
	Cascade        bool               // True when MorphTo(...).Cascade() was set — emit a
	// pre-delete hook on every allowed parent that
	// deletes all polymorphic children pointing at
	// the parent. Application-level cascade.
	SoftDelete     bool               // True when MorphTo(...).SoftDelete() was set —
	// reverse resolves filter parents whose
	// SoftDeleteField is non-null. Per-target
	// detection in ResolveTargets[i].HasSoftDelete.
	SoftDeleteField string            // Parent column name to check (default "deleted_at").
	GQL            bool               // True when MorphTo(...).GQL() was set — emit
	// gqlgen-recognisable union surface.
	GQLUnionName   string             // PascalCase union name (default = PascalCase of
	// MorphName; override via .GQL("CustomName")).
	ChildIDGoType  string             // The child's own ID Go type — used as the
	// eager-load result-map key type. Builtin or
	// custom Ident (e.g. "uuid.UUID").
	ChildIDPkgPath string             // Import path for ChildIDGoType when non-builtin.
	ResolveTargets []resolveTargetRef // Per-parent metadata for the typed resolver
	// (QueryCommentable) — populated from the
	// loaded gen.Graph at preprocess time.
}

// resolveTargetRef carries the per-parent metadata the typed parent
// resolver needs to convert a stringified morph id back to the parent's
// concrete primary key type and dispatch to the right ent client.
type resolveTargetRef struct {
	SchemaName string // The ent schema name (e.g. "Post").
	IDGoType   string // The Go type of the parent's id field — used as
	// BOTH the template branch tag AND the rendered Go
	// type. Builtins: "int", "int64", "string". Custom
	// Go types: their Ident, e.g. "uuid.UUID".
	IDPkgPath  string // Non-builtin import path (e.g.
	// "github.com/google/uuid"). Empty for builtin
	// types. Collected into tmplData.ExtraImports so
	// the generated polymorphic.go imports the package.
	HasSoftDelete bool // True when this target actually has the
	// soft-delete field declared (detected in
	// preprocess). Template skips the IsNil filter
	// when false even if MorphTo.SoftDelete is set.
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
	TypeIsEnum bool   // Whether the target child's type column is a
	// field.Enum (via MixinAllowed). Same meaning as
	// childInfo.TypeIsEnum.
}

// holderInfo describes one MorphedByMany declaration on the holder side
// (e.g. Tag's "posts" back-ref).
type holderInfo struct {
	HolderName       string // The holder schema name (e.g. "Tag").
	FieldName        string // The back-ref method name on the holder (e.g. "posts").
	InverseFieldName string // Auto-emitted method name on the target (e.g. "tags" → post.QueryTags).
	Target           string // The concrete parent schema name (e.g. "Post").
	Pivot            string // The pivot schema name (e.g. "Taggable").
	ThroughName      string // The pivot SQL table name (cosmetic).
	MorphName        string // The relation name on the pivot.
	IDColumn         string // Custom id column override on the pivot.
	TypeColumn       string // Custom type column override on the pivot.
	TargetIDGoType   string // Go type of Target's ID field ("int" / "int64" / "string" / "uuid.UUID" / ...).
	TargetIDPkgPath  string // Import path for the target's ID type when non-builtin.
	HolderIDGoType   string // Go type of Holder's ID field.
	HolderIDPkgPath  string // Import path for the holder's ID type when non-builtin.
	TypeIsEnum       bool   // Whether the pivot's morph-type column was emitted as
	// a field.Enum (via MixinAllowed on the pivot's
	// MorphMixin). Same meaning as childInfo.TypeIsEnum.
}
