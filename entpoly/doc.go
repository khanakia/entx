// Package entpoly provides an ent codegen extension that adds Laravel-style
// polymorphic relationships to ent: MorphOne, MorphMany, MorphTo, and
// MorphedByMany. Declarations live in the schema's Edges() return alongside
// ent's own edge.To / edge.From — the API is intentionally indistinguishable
// from ent's native shape.
//
// A polymorphic relationship lets a single child entity belong to one of N
// parent types via a (id, type) discriminator pair. Example: a Comment can
// belong to a Post or a Video; an Image can belong to any entity that owns
// a featured image.
//
// # Quick start
//
// The child schema needs two ingredients: the MorphMixin (adds the
// discriminator columns) and a MorphTo edge (declares the relation + allowed
// parents):
//
//	type Comment struct{ ent.Schema }
//
//	func (Comment) Mixin() []ent.Mixin {
//	    return []ent.Mixin{
//	        entpoly.MorphMixin("commentable",
//	            entpoly.MixinAllowed(Post.Type, Video.Type), // → field.Enum
//	        ),
//	    }
//	}
//
//	func (Comment) Fields() []ent.Field {
//	    return []ent.Field{field.Text("body")}
//	}
//
//	func (Comment) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphTo("commentable", Post.Type, Video.Type),
//	    }
//	}
//
// Parent schemas only declare back-references — no fields, no mixin:
//
//	func (Post) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphMany("comments", Comment.Type, "commentable"),
//	        entpoly.MorphOne("featured_image", Image.Type, "imageable"),
//	    }
//	}
//
// Many-to-many is wired through a polymorphic pivot (MorphTo on the pivot;
// MorphedByMany.Through on the M2M holder side):
//
//	func (Taggable) Edges() []ent.Edge {
//	    return []ent.Edge{entpoly.MorphTo("taggable", Post.Type, Video.Type)}
//	}
//
//	func (Tag) Edges() []ent.Edge {
//	    return []ent.Edge{
//	        entpoly.MorphedByMany("posts",  Post.Type).Through("taggables", Taggable.Type),
//	        entpoly.MorphedByMany("videos", Video.Type).Through("taggables", Taggable.Type),
//	    }
//	}
//
// # Generated surface
//
// Running `go generate ./ent` with the extension registered emits a sidecar
// polymorphic.go containing:
//
//   - Sealed interface per relation (e.g. CommentCommentableParent) so
//     Set<Morph>(parent) only accepts the AllowedTypes at compile time.
//   - Typed MorphKey constants (e.g. PostMorphKey, VideoMorphKey).
//   - MorphID() / MorphKey() methods on every parent type.
//   - Set<Morph> / Clear<Morph> builder methods on Create / Update /
//     UpdateOne / Mutation for every child.
//   - Typed predicate constructors: <Child><Rel>Is(parent),
//     <Child><Rel>IsType(MorphKey).
//   - Typed forward resolver: comment.QueryCommentable(ctx) returns the
//     sealed interface — caller type-switches over the closed parent set.
//   - Typed back-refs on parents: post.QueryComments() *CommentQuery (many)
//     and post.QueryFeaturedImage(ctx) (*Image, error) (one).
//
// # Morph map (optional)
//
// The string written into the `*_type` column is the morph key. By default
// it is the snake_case name of the ent type ("post", "video") and is
// auto-registered for every parent referenced from a MorphTo edge. Register
// explicit aliases via WithMorphMap in your entc.go only when you want a
// non-default alias — typically to keep persisted "*_type" values stable
// across a Go-side schema rename. See docs/morph-map.md for the rename
// workflow.
//
//	entc.Extensions(entpoly.NewExtension(
//	    entpoly.WithMorphMap(map[string]string{
//	        "post":  "Post",
//	        "video": "Video",
//	    }),
//	))
//
// # Foreign keys
//
// Polymorphic relationships intentionally do NOT emit foreign-key
// constraints (a column cannot reference multiple tables). entpoly does
// emit the type column as a real enum (DB-level CHECK / native ENUM) when
// MixinAllowed is set — this is the strongest constraint SQL allows for a
// multi-target column. Referential integrity (cascade delete) is the
// application's responsibility; pair with the entcascade extension for
// auto-generated cascade helpers.
//
// # Codebase layout
//
// File responsibilities — read top-of-file comments before editing:
//
//	doc.go         this file — package overview + codebase layout
//	edge.go        edge builders (MorphTo / MorphMany / MorphOne / MorphedByMany)
//	               + the markerAnnotation that flags polymorphic edges in
//	               the graph; the schemaName() reflection helper
//	mixin.go       MorphMixin — adds discriminator columns via ent's
//	               official mixin pipeline; MixinAllowed opts into
//	               field.Enum for DB-level enforcement
//	morphmap.go    MorphMap (alias→schemaName) + snake helper +
//	               resolveTarget; pure helpers, no graph mutation
//	extension.go   entc.Extension wiring; three-phase hook chain
//	               (preprocess → next.Generate → generate)
//	preprocess.go  graph mutation phase — strips polymorphic edges from
//	               gen.Type.Edges, validates mixin agreement, populates
//	               polyState; carries the canonical edge-cases table
//	state.go       polyState — the bridge between preprocess and generate;
//	               childInfo / parentInfo / holderInfo + resolveTargetRef
//	               for per-parent ID Go-type info
//	generate.go    sidecar emission — buildTmplData() transforms polyState
//	               into the template-ready tmplData shape, runs the
//	               template, formats + writes ent/polymorphic.go
//	template.go    embeds polymorphic.tmpl via //go:embed
//	templates/polymorphic.tmpl    the actual codegen template — see the
//	                              header comment for nested-range scope
//	                              rules and the $c-binding convention
//	helper/        runtime helpers (Toggle / Sync / SyncWithoutDetach) —
//	               pure set-diff functions, no DB calls
//
// Pipeline order — every codegen pass runs phases in this order:
//
//	  1. Schema load          (ent reads ent/schema/*.go; mixins run here,
//	                           so the discriminator columns are added to
//	                           the loaded schema before any of our code
//	                           sees the graph)
//	  2. preprocess(g)        (our hook — walk gen.Type.Edges, strip the
//	                           polymorphic ones, record each in polyState,
//	                           auto-register parent participants in the
//	                           morph map)
//	  3. next.Generate(g)     (ent's own templates run against the now-
//	                           non-polymorphic graph — emits the normal
//	                           ent client, predicates, builders, etc.)
//	  4. generate(g)          (our hook — render polymorphic.tmpl from
//	                           polyState, format with go/format, write to
//	                           <target>/polymorphic.go)
//
// How to add a new emission to the template:
//
//	1. Add a field to tmplData in generate.go (the template can only see
//	   what buildTmplData populates).
//	2. Populate that field inside buildTmplData by reading from polyState.
//	3. Inside templates/polymorphic.tmpl, range over the new field. If you
//	   need access to the outer Children iterator from a nested range, bind
//	   it with `range $c := .Children` and reference $c inside the inner
//	   block — Go templates do NOT scope `.` upward across nested ranges
//	   (this is the #1 template bug in this codebase; see the inner-range
//	   sites in the template for the working pattern).
//	4. Add or update a runtime test in examples/basic/runtime_test.go to
//	   prove the emitted code compiles AND behaves correctly against a
//	   real (in-memory SQLite) database.
//
// How to add a new edge kind (e.g. MorphToMany):
//
//	1. Add a builder type in edge.go that implements ent.Edge.
//	2. Add a new Kind constant to markerAnnotation (e.g. "morphToMany")
//	   and dispatch on it in preprocess.go's main switch.
//	3. Add a handle<Kind>(t, m) function in preprocess.go that records the
//	   right shape in polyState. If the edge has no concrete target type,
//	   set the descriptor's Type to a placeholder allowed parent (mirror
//	   MorphTo's pattern) so ent's graph builder doesn't error.
//	4. Update tmplData + buildTmplData + the template (see above).
//
// Invariants the codegen depends on — DO NOT break these silently:
//
//	1. Mixin + edge agree on column names, id type, and allowed parents.
//	   MixinAllowed and MorphTo's parent list must match exactly.
//	2. Marker annotation name (MarkerName) is the public contract for
//	   edge identification — renaming breaks every consumer.
//	3. Deterministic codegen — every slice is sorted before iteration,
//	   every map is rendered via sorted-key iteration. Two `go generate`
//	   runs against the same schema must produce byte-identical output.
//	4. The polymorphic.go sidecar lives in the SAME Go package as ent's
//	   generated files — this is what lets us add methods to ent.Post,
//	   ent.CommentCreate, etc. directly.
//
// Testing — see test files for which case each test covers:
//
//	entpoly/edge_test.go         builder API + reflection helper
//	entpoly/edgecase_test.go     edge cases #1–#7 from preprocess.go
//	                             (search "Case #" in the file)
//	entpoly/integration_test.go  full preprocess→generate pipeline
//	entpoly/helper/helper_test.go      Toggle / Sync / SyncWithoutDetach
//	entpoly/examples/basic/runtime_test.go    full end-to-end against
//	                                          a real ent client + SQLite
//
// Design rationale — for any non-obvious choice, the docs at:
//
//	docs/adr-001-type-safety.md       sealed iface vs enum vs both
//	docs/architecture.md              full file-by-file tour
//	docs/laravel-parity.md            Laravel operation → entpoly mapping
//
// answer "why this and not that" with diagrams and alternatives we rejected.
package entpoly
