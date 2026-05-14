// Package runtime hosts the handwritten, framework-side core of enttui.
// Generated code (per-entity, in ent/gen/enttui/) registers entities into
// this runtime via typed EntitySpec values.
//
// Everything in this package MUST be schema-agnostic. No imports of any
// specific ent package allowed.
package runtime

import (
	"context"
	"time"
)

// SortDir is asc/desc.
type SortDir int

const (
	Asc SortDir = iota
	Desc
)

// EdgeKind classifies how an edge is presented in the UI.
type EdgeKind int

const (
	// EdgeDrill is a 1→N relationship presented as "open a new Browser
	// page filtered to these rows."
	EdgeDrill EdgeKind = iota
	// EdgeUpward is a N→1 relationship presented as "jump to the parent's
	// preview" (single entity ref).
	EdgeUpward
)

// EntityRef is an opaque pointer to one row of any registered entity.
// Used as the unit of edge navigation.
type EntityRef struct {
	Kind string // matches EntitySpec.Kind
	ID   string
}

// EntityRefList is a batch of refs of the same kind, the result of an
// EdgeDrill resolver.
type EntityRefList struct {
	Kind string
	IDs  []string
}

// ListOpts is what generated Fetch closures receive.
//
// Scope is a generic string/string bag set on the App via SetScope. The
// runtime never inspects its contents — generated Fetch closures look up
// whichever keys they recognize (e.g. "project_id" for project-scoped
// entities, "tenant_id" for tenant-scoped, etc.). Empty → no scope filter.
type ListOpts struct {
	Filter    string            // substring; matches against fields with Filterable
	SortField string            // matches a Column.Key
	SortDir   SortDir
	Offset    int
	Limit     int
	Scope     map[string]string // arbitrary consumer-defined scope filters
}

// Column describes one displayable field of an entity. Generated code
// produces []Column[T] where T is the ent row type.
type Column[T any] struct {
	Key    string                // stable key (matches ent field name)
	Label  string                // pretty label shown to the user
	Get    func(T) string        // typed accessor — no reflect
	Chip   map[string]string     // optional value→tone color map ("done"→"success")
	Hidden bool                  // never shown
}

// EdgeSpec is one navigable edge from an entity. Generated.
type EdgeSpec[T any] struct {
	Name    string                                      // ent edge name
	Display string                                      // pretty label
	Kind    EdgeKind
	Trigger string                                      // keybinding (e.g. "enter", "p", "c")
	// Count is optional; if set, the preview shows "(N)" next to the edge.
	Count func(ctx context.Context, row T) (int, error)
	// ResolveUpward is set when Kind == EdgeUpward.
	ResolveUpward func(ctx context.Context, row T) (EntityRef, error)
	// ResolveDrill is set when Kind == EdgeDrill.
	ResolveDrill func(ctx context.Context, row T) (EntityRefList, error)
}

// DefaultView captures the entity's preferred sort/filter at first open.
type DefaultView struct {
	SortField string
	SortDir   SortDir
}

// EntitySpec is the typed description of one browsable entity. Generated
// code emits one EntitySpec[T] per ent schema annotated with Browse().
type EntitySpec[T any] struct {
	// Identity
	Kind    string // url-safe ident, e.g. "task"
	Display string // pretty label for the picker
	Group   string // picker group, e.g. "workflow"
	Icon    string // single rune, e.g. "✓"

	// Behavior
	PageSize int
	Default  DefaultView

	// Data
	Fetch func(ctx context.Context, opts ListOpts) (rows []T, total int, err error)

	// Display accessors — the four "hero" fields used everywhere.
	Title  func(T) string
	Body   func(T) string
	Status func(T) string

	// Display accessors — all visible columns in declaration order.
	Columns []Column[T]

	// Created/Updated for the preview header.
	CreatedAt func(T) time.Time
	UpdatedAt func(T) time.Time

	// Navigation
	Edges []EdgeSpec[T]
}
