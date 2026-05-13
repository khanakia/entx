// extension.go — the entc.Extension wiring. This file is the entry point
// callers register in their ent/entc.go via entc.Extensions(...). Everything
// after entc.Generate is invoked goes through the three-phase hook chain
// defined here.
//
// Notes:
//
//   - Hook ordering is load-bearing: preprocess → next.Generate → generate.
//     preprocess mutates the graph BEFORE ent's templates run; if you swap
//     the order, ent emits FK columns + edge methods for our polymorphic
//     edges (because they have a placeholder Type) and the generated code
//     refuses to compile.
//
//   - generate (the sidecar emit) is gated on next.Generate succeeding.
//     If ent's own codegen fails, we skip our pass — emitting against a
//     partial graph just produces confusing compile errors downstream.
//
//   - Options must be idempotent. WithMorphMap merges into the existing
//     map; calling it twice with overlapping keys is allowed and the
//     later call wins. New options should follow the same contract.
//
//   - The Extension struct embeds entc.DefaultExtension so we satisfy the
//     entc.Extension interface even if ent adds new optional methods in
//     a future version. Do not remove the embedding.
//
//   - The `state` field is mutated during preprocess and read during
//     generate. It is a single sequential pipeline; there is no
//     concurrency to guard against today. If we ever parallelise
//     codegen, this field needs synchronisation.
package entpoly

import (
	"maps"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

// Option is the functional-options constructor type used by NewExtension.
// All Options must be idempotent — registering the same option twice must
// produce the same final state, so users can compose option sets safely.
type Option func(*Extension)

// Extension is the entpoly codegen plugin. It implements entc.Extension via
// entc.DefaultExtension embedding and contributes a single hook that runs
// the polymorphic preprocess pass (strip edges, inject fields), forwards to
// ent's standard codegen, and emits the polymorphic.go sidecar file.
//
// Register the extension in your ent/entc.go:
//
//	opts := []entc.Option{
//	    entc.Extensions(entpoly.NewExtension(
//	        entpoly.WithMorphMap(map[string]string{
//	            "post":  "Post",
//	            "video": "Video",
//	        }),
//	    )),
//	}
//	if err := entc.Generate("./schema", config, opts...); err != nil { ... }
type Extension struct {
	entc.DefaultExtension

	// morphMap is the user-configured alias-to-schema-name map, merged
	// at NewExtension time and not mutated afterwards. preprocess()
	// reads this into the per-run polyState.
	morphMap MorphMap

	// state is built fresh by preprocess() during each codegen pass and
	// consumed by generate(). It is the bridge between the graph-mutation
	// pass and the sidecar-emit pass.
	state *polyState

	// gqlSchemaPath is the optional output path for the GraphQL union
	// schema fragment. Set via WithGQLSchemaFile(...). Empty means
	// "skip — user writes the union declarations by hand."
	gqlSchemaPath string
}

// NewExtension constructs an Extension and applies every functional option
// in order. Options that mutate the same field overwrite previous values —
// later options win — which is the standard Go functional-options idiom.
func NewExtension(opts ...Option) *Extension {
	e := &Extension{morphMap: MorphMap{}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// WithGQLSchemaFile configures the output path for the GraphQL union
// schema fragment emitted alongside polymorphic.go. When set AND at
// least one MorphTo declares .GQL(), entpoly writes a small .graphql
// file containing `union X = Y | Z` declarations for every GQL-
// enabled relation in the project. Otherwise the user writes the
// union declarations manually in their schema.graphqls.
//
//	entc.Extensions(entpoly.NewExtension(
//	    entpoly.WithGQLSchemaFile("./graph/polymorphic.graphql"),
//	))
//
// The path is relative to the directory where `go generate` runs.
func WithGQLSchemaFile(path string) Option {
	return func(e *Extension) { e.gqlSchemaPath = path }
}

// WithMorphMap registers explicit string aliases mapping morph keys
// (persisted in the "*_type" column) to ent schema type names. Calling
// WithMorphMap multiple times is additive — entries from later calls merge
// into the existing map and overwrite on key collision.
//
// The morph map is the contract between persisted data and Go types. Keep
// the left-hand-side keys stable for the lifetime of your application; the
// right-hand-side schema names can be renamed freely as long as the map
// stays in sync.
func WithMorphMap(m map[string]string) Option {
	return func(e *Extension) {
		maps.Copy(e.morphMap, m)
	}
}

// Hooks satisfies the entc.Extension interface. Returns a single hook that
// runs the entpoly preprocess pass (strip polymorphic edges, inject the
// discriminator id+type fields onto the child types), forwards to ent's
// own codegen, and then writes the sidecar file.
func (e *Extension) Hooks() []gen.Hook {
	return []gen.Hook{e.hook}
}

// hook wires the three-phase codegen pipeline together:
//
//  1. preprocess  — mutate gen.Graph: strip poly edges, inject discriminator
//     fields, populate e.state with everything sidecar codegen needs.
//  2. next        — ent's own templates run on the now-non-polymorphic graph.
//     If ent's pass fails, entpoly does not emit the sidecar — emitting
//     against a partial graph would just produce confusing compile errors
//     downstream.
//  3. generate    — write the polymorphic.go sidecar from e.state.
func (e *Extension) hook(next gen.Generator) gen.Generator {
	return gen.GenerateFunc(func(g *gen.Graph) error {
		if err := e.preprocess(g); err != nil {
			return err
		}
		if err := next.Generate(g); err != nil {
			return err
		}
		return e.generate(g)
	})
}
