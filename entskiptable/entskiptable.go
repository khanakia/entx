// Package entskiptable provides a reusable ent auto-migration DiffHook
// that EXCLUDES selected tables from migration.
//
// Use case: a table is owned by another module/service (a different ent
// client, a separate microservice, a hand-managed table) but you still
// model it as a table-typed ent.Schema in this client so you can query
// it / traverse edges to it. ent's auto-migration would then try to
// reshape that foreign table to match your local schema (e.g.
// `ALTER COLUMN ... DROP NOT NULL`). That is destructive cross-owner
// DDL.
//
// ent's own schema-level escape hatches don't fit this case:
//   - entsql.Annotation{Skip:true} breaks codegen when other tables
//     hold edges to the skipped type (migrate template nil-derefs).
//   - ent.View can't carry foreign-key edges (ent <= v0.14.x).
//
// A migration DiffHook is the only approach that keeps codegen + edges
// intact. This package packages that hook generically:
//
//	client.Schema.Create(ctx,
//	    migrate.WithForeignKeys(false),
//	    schema.WithDiffHook(entskiptable.SkipHook(
//	        entskiptable.Any(
//	            entskiptable.ByPrefix("auth_"),
//	            entskiptable.ByName("billing_accounts"),
//	        ),
//	    )),
//	)
//
// The hook can READ the excluded tables (SELECT) — it only strips
// schema CHANGES (Add/Drop/Modify/Rename table) targeting them, so ent
// never emits DDL for them.
package entskiptable

import (
	"strings"

	atlas "ariga.io/atlas/sql/schema"
	entschema "entgo.io/ent/dialect/sql/schema"
)

// Predicate reports whether the given table name must be excluded from
// migration (no DDL emitted for it).
type Predicate func(table string) bool

// ByName excludes the exact table names given.
func ByName(names ...string) Predicate {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(table string) bool {
		_, ok := set[table]
		return ok
	}
}

// ByPrefix excludes any table whose name starts with one of the given
// prefixes (e.g. an entire module-owned namespace like "auth_").
func ByPrefix(prefixes ...string) Predicate {
	return func(table string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(table, p) {
				return true
			}
		}
		return false
	}
}

// Any combines predicates with OR: a table is excluded if ANY predicate
// matches. Nil predicates are ignored.
func Any(preds ...Predicate) Predicate {
	return func(table string) bool {
		for _, p := range preds {
			if p != nil && p(table) {
				return true
			}
		}
		return false
	}
}

// changeTable returns the table name an atlas change targets, or "" for
// changes that are not table-scoped (those are always kept).
func changeTable(c atlas.Change) string {
	switch t := c.(type) {
	case *atlas.AddTable:
		return t.T.Name
	case *atlas.DropTable:
		return t.T.Name
	case *atlas.ModifyTable:
		return t.T.Name
	case *atlas.RenameTable:
		return t.From.Name
	default:
		return ""
	}
}

// SkipHook returns an ent migration DiffHook that drops every schema
// change whose target table matches skip. If skip is nil the hook is a
// no-op (all changes pass through).
func SkipHook(skip Predicate) entschema.DiffHook {
	return func(next entschema.Differ) entschema.Differ {
		return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
			changes, err := next.Diff(current, desired)
			if err != nil {
				return nil, err
			}
			if skip == nil {
				return changes, nil
			}
			kept := make([]atlas.Change, 0, len(changes))
			for _, c := range changes {
				if tbl := changeTable(c); tbl != "" && skip(tbl) {
					continue // externally owned — emit no DDL for it
				}
				kept = append(kept, c)
			}
			return kept, nil
		})
	}
}
