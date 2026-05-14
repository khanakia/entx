// Package-level note: this file contains the graph-mutation phase of the
// codegen pipeline. Most of the user-facing safety guarantees of entpoly
// live here — every "what happens when the user does X wrong" decision
// is made by one of the handlers below.
//
// ─────────────────────────────────────────────────────────────────────────
// Edge cases handled in this file
// ─────────────────────────────────────────────────────────────────────────
//
//   #   Case                                            Where handled
//   ──  ──────────────────────────────────────────────  ────────────────────────
//   1   MorphTo("x") with no parents                    handleMorphTo: errors at
//                                                       preprocess w/ hint to
//                                                       pass at least one X.Type
//
//   2   Mixin column name vs edge override mismatch     handleMorphTo: error
//                                                       message includes the
//                                                       correct MixinIDColumn /
//                                                       MixinTypeColumn hint
//
//   3   Multiple MorphTo edges on one schema            preprocess loop:
//                                                       independent dispatch per
//                                                       edge → both recorded as
//                                                       separate Children
//
//   4   Self-referential polymorphic (Comment → Comment) handleMorphTo: the host
//                                                       type appearing in its
//                                                       own AllowedTypes is
//                                                       auto-registered in
//                                                       the morph map
//
//   5   MorphedByMany w/o .Through()                    handleHolder: errors at
//                                                       preprocess w/ hint
//
//   6   MorphedByMany w/ no parent type                 handleHolder: errors at
//                                                       preprocess
//
//   7   Mixin column overrides agree with edge          handleMorphTo: column
//                                                       presence check is the
//                                                       agreement check
//
//   8   Non-polymorphic edges preserved alongside poly  preprocess loop: edges
//                                                       without the marker
//                                                       annotation are kept
//                                                       verbatim in t.Edges
//
//   9   Parent participants auto-registered in morph    preprocess tail: types
//       map even without an explicit alias              referenced as parents
//                                                       but missing from the
//                                                       map get snake_case
//                                                       aliases
//
//   10  No-op when graph has no polymorphic edges       generate: hasParticipants
//                                                       check short-circuits the
//                                                       sidecar emit entirely
//
//   11  Ghost FK columns left behind by ent's edge      preprocess tail: walk every
//       processor after our edge strip                  type's ForeignKeys, drop
//                                                       entries whose Edge has our
//                                                       marker, also drop the FK
//                                                       field from t.Fields. Result:
//                                                       no leftover `post_comments`
//                                                       *int on the child struct.
//
//   12  AllowedTypes drift between MixinAllowed enum    handleMorphTo: cross-check
//       and MorphTo's parent list                       the typeCol's Enums against
//                                                       AllowedTypes; report symmetric
//                                                       diff with remediation hint.
//
//   13  Non-builtin parent ID Go type                   idGoType helper captures
//       (uuid.UUID, ULID, etc)                          both Ident + PkgPath;
//                                                       collected into
//                                                       tmplData.ExtraImports so the
//                                                       generated file imports the
//                                                       package (e.g. uuid). Template
//                                                       branches on "uuid.UUID" for
//                                                       the strconv → uuid.Parse swap.
//
//   14  Cascade() pre-delete hook emission              handleMorphTo records the
//                                                       flag; template emits one
//                                                       cascade hook per (child,
//                                                       allowed parent) pair on the
//                                                       PARENT type, wired into
//                                                       RegisterPolyHooks. Hook runs
//                                                       BEFORE next.Generate (delete)
//                                                       so children leave before
//                                                       parent.
//
//   15  SoftDelete() per-parent auto-detection         handleMorphTo scans each
//                                                       allowed parent's Fields for
//                                                       the configured soft-delete
//                                                       column; HasSoftDelete on
//                                                       resolveTargetRef drives a
//                                                       per-target IsNil filter in
//                                                       the resolver + eager-load.
//
//   16  <Child><Rel>On<Parent> sub-query predicate     Per (child × allowed parent)
//       constructor                                     helper emitted in the
//                                                       template; builds a sub-
//                                                       SELECT of parent IDs scoped
//                                                       to the morph type. SoftDelete
//                                                       auto-included in the sub.
//                                                       Composes with comment.Or /
//                                                       And for multi-type matches.
//
//   17  GraphQL union emission (Option B / ADR-002)     handleMorphTo records the
//                                                       GQL flag + optional union
//                                                       name; template emits Go-side
//                                                       union surface (type alias +
//                                                       exported markers + resolver
//                                                       helper); generate.go writes
//                                                       a sidecar .graphql file when
//                                                       WithGQLSchemaFile is set.
//
//   18  Type column is field.String (no MixinAllowed) handleMorphTo detects the
//                                                       presence of Field.Enums on the
//                                                       type column and threads TypeIsEnum
//                                                       through childInfo / parentInfo /
//                                                       holderInfo. Template branches at
//                                                       every cast site so plain-string
//                                                       columns emit string(MorphKey)
//                                                       directly while enum columns keep
//                                                       the named-type conversion.
//
//   19  MorphedByMany.Through() morph-name from pivot   Pre-pass over g.Nodes builds
//                                                       pivotMorph[typeName]=morphName
//                                                       BEFORE any edges are stripped.
//                                                       handleHolder then resolves:
//                                                       explicit .MorphName(...) >
//                                                       pivot's MorphTo MorphName >
//                                                       singularise(ThroughName) fallback.
//                                                       Closes the gap where the pivot
//                                                       table name didn't share a stem
//                                                       with the morph noun.
//
//   20  Composite-index storage-key override            MixinIndexName(name) sets
//                                                       index.Fields(typeCol, idCol)
//                                                       .StorageKey(name) on the mixin.
//                                                       Lets two ent modules sharing a
//                                                       database avoid collisions when
//                                                       both declare the same Go entity
//                                                       name and morph relation.
//                                                       Implemented in mixin.go, not
//                                                       preprocess.
//
// Tests for each case live in edgecase_test.go and integration_test.go;
// search by the case number in those files to find the exercising tests.
package entpoly

import (
	"fmt"

	"entgo.io/ent/entc/gen"
)

// preprocess walks the loaded graph, identifies polymorphic edges by the
// marker annotation, strips them out of gen.Type.Edges so ent's templates
// do not try to emit FK constraints or standard edge methods for them,
// and records every relation in the per-run polyState for sidecar codegen
// to consume.
//
// The discriminator id+type columns themselves are NOT injected here —
// they come from the MorphMixin the user places on the child schema.
// preprocess only verifies the mixin contributed them; missing columns
// produce a clear error that points the user back at the missing
// MorphMixin call (case #2 in the edge-cases table above).
//
// The state is stashed on the extension so the sidecar template renderer
// can read it without re-walking the graph. All edge stripping happens
// before ent's templates run; ent codegen therefore sees a graph that
// looks entirely non-polymorphic.
//
// This function is called from the gen.Hook middleware in extension.go
// (preprocess → next.Generate → generate sidecar).
func (e *Extension) preprocess(g *gen.Graph) error {
	e.state = &polyState{
		Package:  g.Config.Package,
		MorphMap: map[string]string{},
	}

	// Seed the morph map with explicit user-supplied aliases.
	for k, v := range e.morphMap {
		e.state.MorphMap[k] = v
	}

	// Pre-pass: index each type's MorphTo morph name BEFORE we strip
	// any edges. handleHolder needs this so a MorphedByMany whose
	// Through(...) pivot table doesn't share a stem with the pivot's
	// MorphTo morph name (e.g. "source_links" → "source_link" vs the
	// real "sourceable") still resolves the right discriminator
	// columns. Doing it lazily inside handleHolder would be iteration-
	// order dependent — the pivot's edges may already be stripped by
	// the time the holder is processed.
	pivotMorph := map[string]string{}
	for _, t := range g.Nodes {
		for _, ed := range t.Edges {
			if ed.Annotations == nil {
				continue
			}
			raw, ok := ed.Annotations[MarkerName]
			if !ok {
				continue
			}
			m, ok := decodeMarkerAny(raw)
			if !ok || m.Kind != "morphTo" || m.MorphName == "" {
				continue
			}
			pivotMorph[t.Name] = m.MorphName
		}
	}
	e.state.pivotMorph = pivotMorph

	for _, t := range g.Nodes {
		kept := t.Edges[:0]
		for _, ed := range t.Edges {
			if ed.Annotations == nil {
				kept = append(kept, ed)
				continue
			}
			// Identify polymorphic edges via the marker annotation in
			// the edge's annotations map. ent's pipeline JSON-encodes
			// annotations into gen.Edge.Annotations.
			raw, ok := ed.Annotations[MarkerName]
			if !ok {
				kept = append(kept, ed)
				continue
			}
			m, ok := decodeMarkerAny(raw)
			if !ok {
				return fmt.Errorf("entpoly: edge %s.%s carries malformed marker", t.Name, ed.Name)
			}

			// Dispatch on the polymorphic kind. Each handler records
			// what it needs in e.state and never re-adds the edge to
			// kept, effectively stripping it from the graph.
			switch m.Kind {
			case "morphTo":
				if err := e.handleMorphTo(g, t, m); err != nil {
					return err
				}
			case "morphMany":
				e.handleParent(g, t, m)
			case "morphOne":
				e.handleParent(g, t, m)
			case "morphedByMany":
				if err := e.handleHolder(g, t, m); err != nil {
					return err
				}
			default:
				return fmt.Errorf("entpoly: unknown marker kind %q on %s.%s", m.Kind, t.Name, ed.Name)
			}
		}
		t.Edges = kept
	}

	// Strip ghost FK columns ent auto-added for the polymorphic edges
	// we just removed. ent's edge processor adds an entry to the
	// target's gen.Type.ForeignKeys AND a hidden field on the target's
	// gen.Type.Fields for every edge.To, BEFORE our hook runs. The
	// strip above removes the edge from Edges, but the FK and its
	// underlying field linger as ghost state — they show up as
	// unexported fields on the generated entity struct (e.g.
	// "post_comments *int" on Comment) and confuse readers of the
	// generated code.
	//
	// We identify ghost FKs by walking each type's ForeignKeys and
	// checking whether the linked Edge carries our marker annotation.
	// Removing both the FK entry and its underlying field leaves the
	// generated struct clean.
	for _, t := range g.Nodes {
		keptFKs := t.ForeignKeys[:0]
		ghostFieldNames := map[string]struct{}{}
		for _, fk := range t.ForeignKeys {
			if fk.Edge == nil || fk.Edge.Annotations == nil {
				keptFKs = append(keptFKs, fk)
				continue
			}
			if _, isPoly := fk.Edge.Annotations[MarkerName]; isPoly {
				// Record the ghost field name so we can also drop it
				// from t.Fields below; do not re-add this FK entry.
				if fk.Field != nil {
					ghostFieldNames[fk.Field.Name] = struct{}{}
				}
				continue
			}
			keptFKs = append(keptFKs, fk)
		}
		t.ForeignKeys = keptFKs
		if len(ghostFieldNames) > 0 {
			keptFields := t.Fields[:0]
			for _, f := range t.Fields {
				if _, ghost := ghostFieldNames[f.Name]; ghost {
					continue
				}
				keptFields = append(keptFields, f)
			}
			t.Fields = keptFields
		}
	}

	// Auto-register any parent participant into the morph map so the
	// runtime morph lookup works for back-refs that did not have an
	// explicit alias.
	for _, name := range e.state.parentNames() {
		if _, ok := lookupAlias(e.state.MorphMap, name); !ok {
			e.state.MorphMap[snake(name)] = name
		}
	}

	return nil
}

// handleMorphTo records a child-side declaration. The discriminator
// columns themselves are added via the MorphMixin the user places on
// the schema; preprocess only verifies they exist and records the
// metadata the sidecar template needs.
//
// Validating that the mixin was registered is the early-warning path —
// without it, the generated Set<Morph>/Clear<Morph> methods would
// reference field setters that ent never generated.
// findTypeByName locates a *gen.Type by its schema name. Linear scan;
// the graph is small. Returns nil when no matching type exists — the
// caller treats that as "unknown target" and falls back to a string ID
// for the resolver branch.
func (e *Extension) findTypeByName(g *gen.Graph, name string) *gen.Type {
	for _, t := range g.Nodes {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// idGoType returns the Go-side type name + import path for a *gen.Type's
// ID field. Used to drive the typed resolver / batch-load / map-key
// rendering across the codegen.
//
// Result interpretations:
//
//   - Builtin (`int`, `int64`, `string`): goType is the builtin name,
//     pkgPath is empty. Template strconv branches dispatch on goType.
//   - Custom Go-typed PK (e.g. uuid.UUID): goType is the Ident
//     ("uuid.UUID"), pkgPath is the import path
//     ("github.com/google/uuid"). The template renders goType verbatim
//     in map keys / parameter types and adds pkgPath to the import set.
//
// Returns ("string", "") when t is nil or has no ID — defensive
// default so the template falls through to a pass-through code path.
func idGoType(t *gen.Type) (goType, pkgPath string) {
	if t == nil || t.ID == nil || t.ID.Type == nil {
		return "string", ""
	}
	info := t.ID.Type
	// Custom Go types (UUID, ULID, named int aliases) carry an Ident
	// + PkgPath. info.Type.String() returns the underlying SQL kind
	// (e.g. "[16]byte" for UUID) — useless for rendering — so prefer
	// the Ident when set.
	if info.Ident != "" {
		return info.Ident, info.PkgPath
	}
	return info.String(), ""
}

func (e *Extension) handleMorphTo(g *gen.Graph, t *gen.Type, m *markerAnnotation) error {
	// Builder-time validation that should have caught this — the edge
	// must declare at least one allowed parent type. Without parents
	// the relation is meaningless and the edge's placeholder Type
	// resolves to a non-existent schema, breaking graph construction.
	if len(m.AllowedTypes) == 0 {
		return fmt.Errorf(
			"entpoly: schema %s declares MorphTo(%q) with no allowed parent types — "+
				"pass at least one X.Type argument (e.g. MorphTo(%q, Post.Type, Video.Type))",
			t.Name, m.MorphName, m.MorphName,
		)
	}

	idCol := m.idColumn()
	typeCol := m.typeColumn()

	// Verify the mixin actually contributed the discriminator columns.
	// Users who forget the MorphMixin or supply a column-name override
	// on the edge that does not agree with the mixin would otherwise
	// see a confusing downstream compile failure inside polymorphic.go.
	hasID, hasType := false, false
	for _, f := range t.Fields {
		if f.Name == idCol {
			hasID = true
		}
		if f.Name == typeCol {
			hasType = true
		}
	}
	if !hasID || !hasType {
		// Build a tailored message based on whether the edge override
		// the user might be using contradicts the mixin defaults.
		hint := fmt.Sprintf("entpoly.MorphMixin(%q)", m.MorphName)
		if m.IDColumn != "" || m.TypeColumn != "" {
			hint = fmt.Sprintf(
				"entpoly.MorphMixin(%q",
				m.MorphName,
			)
			if m.IDColumn != "" {
				hint += fmt.Sprintf(", entpoly.MixinIDColumn(%q)", m.IDColumn)
			}
			if m.TypeColumn != "" {
				hint += fmt.Sprintf(", entpoly.MixinTypeColumn(%q)", m.TypeColumn)
			}
			hint += ")"
		}
		return fmt.Errorf(
			"entpoly: schema %s declares MorphTo(%q) but is missing column %q or %q — "+
				"add %s to the schema's Mixin() return (overrides on the edge must agree with the mixin)",
			t.Name, m.MorphName, idCol, typeCol, hint,
		)
	}

	// AllowedTypes drift linter — when the type column was emitted as a
	// field.Enum (via MixinAllowed), the enum values land on the column
	// as gen.Field.Enums. Cross-check that set against the edge's
	// AllowedTypes list. A mismatch (extra/missing parent on one side)
	// produces a clear error pointing at the side that needs updating,
	// rather than a confusing downstream validator failure at runtime.
	for _, f := range t.Fields {
		if f.Name != typeCol || len(f.Enums) == 0 {
			continue
		}
		// The mixin stores values in snake_case morph-key form (e.g.
		// "post", "video"). The edge stores AllowedTypes in schema-name
		// form (e.g. "Post", "Video"). Compare snake_case ↔ snake_case.
		mixinSet := map[string]struct{}{}
		for _, e := range f.Enums {
			mixinSet[e.Value] = struct{}{}
		}
		edgeSet := map[string]struct{}{}
		for _, name := range m.AllowedTypes {
			edgeSet[snake(name)] = struct{}{}
		}
		// Find the asymmetric differences and report both.
		var missingFromMixin, missingFromEdge []string
		for k := range edgeSet {
			if _, ok := mixinSet[k]; !ok {
				missingFromMixin = append(missingFromMixin, k)
			}
		}
		for k := range mixinSet {
			if _, ok := edgeSet[k]; !ok {
				missingFromEdge = append(missingFromEdge, k)
			}
		}
		if len(missingFromMixin) > 0 || len(missingFromEdge) > 0 {
			return fmt.Errorf(
				"entpoly: schema %s MorphMixin(%q) allowed list and MorphTo edge AllowedTypes have drifted apart — "+
					"missing from MixinAllowed: %v; missing from MorphTo: %v "+
					"(update both sides to declare the same parent set)",
				t.Name, m.MorphName, missingFromMixin, missingFromEdge,
			)
		}
		break
	}

	// Look up the child's own ID Go type — used as the map-key type in
	// the eager-load result struct. For custom Go-typed PKs (e.g.
	// uuid.UUID) idGoType also returns the import path so the
	// generated file imports the package.
	childIDType, childIDPkg := idGoType(t)

	// Resolve each allowed parent's ID Go type. The typed resolver
	// (QueryCommentable), the eager-load batched IN(...) call, and
	// the M2M holder back-ref all need to convert the persisted morph
	// id string back to the parent's real PK type. We record both the
	// Go type name (for rendering) and the import path (for the import
	// block) per allowed parent so each branch picks the right parse.
	//
	// SoftDelete is auto-detected per-target: scan the target's
	// Fields for the configured soft-delete column. Targets without
	// the column pass through unfiltered (HasSoftDelete=false) even
	// when MorphTo.SoftDelete is enabled.
	softField := m.SoftDeleteField
	if softField == "" {
		softField = "deleted_at"
	}
	targets := make([]resolveTargetRef, 0, len(m.AllowedTypes))
	for _, name := range m.AllowedTypes {
		tt := e.findTypeByName(g, name)
		gt, pkg := idGoType(tt)
		ref := resolveTargetRef{
			SchemaName: name,
			IDGoType:   gt,
			IDPkgPath:  pkg,
		}
		if m.SoftDelete && tt != nil {
			for _, f := range tt.Fields {
				if f.Name == softField {
					ref.HasSoftDelete = true
					break
				}
			}
		}
		targets = append(targets, ref)
	}

	// Detect whether the type column is a real field.Enum (via
	// MixinAllowed). When it is, ent generates a named string type
	// <pkg>.<TypeField> the template can use as a type conversion.
	// When it is not, the same identifier is the predicate-EQ
	// shortcut function — wrapping a value with it is a function call,
	// not a cast, and the generated code fails to compile.
	typeIsEnum := false
	for _, f := range t.Fields {
		if f.Name == typeCol && len(f.Enums) > 0 {
			typeIsEnum = true
			break
		}
	}

	e.state.Children = append(e.state.Children, childInfo{
		TypeName:       t.Name,
		MorphName:      m.MorphName,
		IDColumn:       idCol,
		TypeColumn:     typeCol,
		TypeIsEnum:     typeIsEnum,
		IDType:         m.IDType,
		AllowedTypes:   m.AllowedTypes,
		Required:       m.Required,
		Touch:           m.Touch,
		TouchField:      m.TouchField,
		Cascade:         m.Cascade,
		SoftDelete:      m.SoftDelete,
		SoftDeleteField: softField,
		GQL:             m.GQL,
		GQLUnionName:    m.GQLUnionName,
		ChildIDGoType:   childIDType,
		ChildIDPkgPath:  childIDPkg,
		ResolveTargets:  targets,
	})

	// MorphTo's allowed parents implicitly participate in the morph map.
	// Register their snake_case aliases unless an explicit override is
	// already present.
	for _, name := range m.AllowedTypes {
		if _, ok := lookupAlias(e.state.MorphMap, name); !ok {
			e.state.MorphMap[snake(name)] = name
		}
	}

	return nil
}

// handleParent records a MorphOne / MorphMany back-reference on the parent
// type. The hosting type itself becomes a parent participant; the morph
// map auto-registers the host if no explicit alias exists.
func (e *Extension) handleParent(g *gen.Graph, t *gen.Type, m *markerAnnotation) {
	// Look up the target child's type column to detect whether it was
	// emitted as a field.Enum. The back-ref accessors emitted from
	// parentInfo cast or skip-cast on that flag the same way the
	// child-side methods do.
	typeIsEnum := false
	typeCol := m.TypeColumn
	if typeCol == "" {
		typeCol = m.MorphName + "_type"
	}
	if tt := e.findTypeByName(g, m.Target); tt != nil {
		for _, f := range tt.Fields {
			if f.Name == typeCol && len(f.Enums) > 0 {
				typeIsEnum = true
				break
			}
		}
	}
	e.state.Parents = append(e.state.Parents, parentInfo{
		ParentName: t.Name,
		FieldName:  m.FieldName,
		Target:     m.Target,
		MorphName:  m.MorphName,
		Kind:       m.Kind,
		IDColumn:   m.IDColumn,
		TypeColumn: m.TypeColumn,
		TypeIsEnum: typeIsEnum,
	})
	e.state.parents = append(e.state.parents, t.Name)
}

// handleHolder records a MorphedByMany M2M back-reference on the holder
// (e.g. Tag). The concrete parent (e.g. Post) participates in the morph
// map. Validates that .Through(...) was actually called — without a
// pivot, the M2M relation has nothing to route through.
func (e *Extension) handleHolder(g *gen.Graph, t *gen.Type, m *markerAnnotation) error {
	if m.Through == "" {
		return fmt.Errorf(
			"entpoly: schema %s declares MorphedByMany(%q, ...) but missing .Through(...) — "+
				"add .Through(%q, <Pivot>.Type) to the edge builder",
			t.Name, m.FieldName, t.Name+"s",
		)
	}
	if m.Target == "" {
		return fmt.Errorf(
			"entpoly: schema %s declares MorphedByMany with no parent type — "+
				"pass the parent's X.Type as the second argument",
			t.Name,
		)
	}
	// Look up target's + holder's ID Go types so both back-ref methods
	// (holder → target AND target → holder) can emit the right
	// strconv conversion / IDIn call. idGoType also surfaces the
	// import path for custom Go-typed PKs (uuid.UUID etc.).
	targetIDType, targetIDPkg := idGoType(e.findTypeByName(g, m.Target))
	holderIDType, holderIDPkg := idGoType(e.findTypeByName(g, t.Name))

	// Default inverse field name = snake-case of holder + "s". User can
	// override via InverseName(...) on the builder when the plural is
	// irregular ("Category" → "categories").
	inverse := m.InverseFieldName
	if inverse == "" {
		inverse = snake(t.Name) + "s"
	}

	// Resolve the morph name. Precedence:
	//   1. Explicit .MorphName(...) on the builder (caller knows best).
	//   2. The pivot type's own MorphTo annotation (the discriminator
	//      columns it actually emits are named after that morph name).
	//   3. singularise(ThroughName) — Laravel "taggables" → "taggable"
	//      convention; only correct when the pivot table name shares a
	//      stem with the morph noun.
	//
	// (2) catches the bug where Through("source_links", SourceLink.Type)
	// would otherwise default to "source_link" while the pivot's actual
	// MorphTo("sourceable", ...) means the columns are "sourceable_*".
	pt := e.findTypeByName(g, m.Through)
	if m.MorphName == "" {
		if name := e.state.pivotMorph[m.Through]; name != "" {
			m.MorphName = name
		}
	}
	if m.MorphName == "" {
		m.MorphName = singularise(m.ThroughName)
	}

	// Detect whether the pivot's morph-type column was emitted as a
	// field.Enum (via MixinAllowed on the pivot's MorphMixin). Drives
	// whether the back-ref methods cast through <pivot>.<TypeField> or
	// pass the raw string.
	typeIsEnum := false
	pivotTypeCol := m.TypeColumn
	if pivotTypeCol == "" {
		pivotTypeCol = m.MorphName + "_type"
	}
	if pt != nil {
		for _, f := range pt.Fields {
			if f.Name == pivotTypeCol && len(f.Enums) > 0 {
				typeIsEnum = true
				break
			}
		}
	}

	e.state.Holders = append(e.state.Holders, holderInfo{
		HolderName:       t.Name,
		FieldName:        m.FieldName,
		InverseFieldName: inverse,
		Target:           m.Target,
		Pivot:            m.Through,
		ThroughName:      m.ThroughName,
		MorphName:        m.MorphName,
		IDColumn:         m.IDColumn,
		TypeColumn:       m.TypeColumn,
		TargetIDGoType:   targetIDType,
		TargetIDPkgPath:  targetIDPkg,
		HolderIDGoType:   holderIDType,
		HolderIDPkgPath:  holderIDPkg,
		TypeIsEnum:       typeIsEnum,
	})
	e.state.parents = append(e.state.parents, m.Target)
	return nil
}

// decodeMarkerAny accepts either a typed markerAnnotation or the
// JSON-shaped map value that ent's annotation pipeline produces, and
// returns the typed struct. Mirrors the dual-shape decoder in edge.go's
// decodeMarker but operates on a single raw value rather than a slice.
func decodeMarkerAny(raw any) (*markerAnnotation, bool) {
	if m, ok := raw.(markerAnnotation); ok {
		return &m, true
	}
	b, err := jsonMarshal(raw)
	if err != nil {
		return nil, false
	}
	var m markerAnnotation
	if err := jsonUnmarshal(b, &m); err != nil {
		return nil, false
	}
	return &m, true
}
