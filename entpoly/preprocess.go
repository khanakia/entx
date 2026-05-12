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
				e.handleParent(t, m)
			case "morphOne":
				e.handleParent(t, m)
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

	// Resolve each allowed parent's ID Go type by looking it up in the
	// loaded graph. This lets the typed resolver (QueryCommentable)
	// emit the right strconv conversion for each branch without having
	// to encode the parent's ID shape in the schema declaration.
	targets := make([]resolveTargetRef, 0, len(m.AllowedTypes))
	for _, name := range m.AllowedTypes {
		ref := resolveTargetRef{SchemaName: name, IDGoType: "string"}
		if tt := e.findTypeByName(g, name); tt != nil && tt.ID != nil && tt.ID.Type != nil {
			ref.IDGoType = tt.ID.Type.String()
		}
		targets = append(targets, ref)
	}

	e.state.Children = append(e.state.Children, childInfo{
		TypeName:       t.Name,
		MorphName:      m.MorphName,
		IDColumn:       idCol,
		TypeColumn:     typeCol,
		IDType:         m.IDType,
		AllowedTypes:   m.AllowedTypes,
		ResolveTargets: targets,
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
func (e *Extension) handleParent(t *gen.Type, m *markerAnnotation) {
	e.state.Parents = append(e.state.Parents, parentInfo{
		ParentName: t.Name,
		FieldName:  m.FieldName,
		Target:     m.Target,
		MorphName:  m.MorphName,
		Kind:       m.Kind,
		IDColumn:   m.IDColumn,
		TypeColumn: m.TypeColumn,
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
	// Look up the target's ID Go type so the holder back-ref method can
	// emit the right strconv conversion (parsing the stringified pivot
	// taggable_id back to the parent's real PK type).
	targetIDType := "string"
	if tt := e.findTypeByName(g, m.Target); tt != nil && tt.ID != nil && tt.ID.Type != nil {
		targetIDType = tt.ID.Type.String()
	}

	e.state.Holders = append(e.state.Holders, holderInfo{
		HolderName:     t.Name,
		FieldName:      m.FieldName,
		Target:         m.Target,
		Pivot:          m.Through,
		ThroughName:    m.ThroughName,
		MorphName:      m.MorphName,
		IDColumn:       m.IDColumn,
		TypeColumn:     m.TypeColumn,
		TargetIDGoType: targetIDType,
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
