# entskiptable

[![Go Reference](https://pkg.go.dev/badge/github.com/khanakia/entx/entskiptable.svg)](https://pkg.go.dev/github.com/khanakia/entx/entskiptable)
[![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8)](https://go.dev)
[![ent v0.14.x](https://img.shields.io/badge/ent-v0.14.x-7e3aed)](https://entgo.io)

**Exclude externally-owned tables from [ent](https://entgo.io) auto-migration.** A tiny, composable migration `DiffHook` that stops `client.Schema.Create` from emitting `CREATE` / `ALTER` / `DROP` DDL against tables your ent client only *reads* — tables owned by another service, another ent client, a database view, or a hand-managed migration. Code-generation and foreign-key edges stay fully intact.

```bash
go get github.com/khanakia/entx/entskiptable
```

## What & why

You model a table as a normal `ent.Schema` so you can **query it and traverse edges to it**, but it is actually **owned by something else** — a different ent client, a separate service, a database view, or a manually-managed table. ent's auto-migration doesn't know that, so the next `client.Schema.Create()` reshapes the foreign table to match your local schema:

```sql
ALTER TABLE "auth_users"
  ALTER COLUMN "email" DROP NOT NULL,
  ALTER COLUMN "status" SET DEFAULT 'available';
```

That is destructive, cross-owner DDL on a table you don't own.

ent's built-in escape hatches don't cover this case:

| Approach | Problem |
| --- | --- |
| `entsql.Annotation{Skip: true}` | Breaks code-generation when **other tables hold edges** to the skipped type (the migration template nil-dereferences). |
| `ent.View` schema | A view-typed schema **cannot carry foreign-key edges** (ent ≤ v0.14.x) — you lose the relationships you modeled the table for. |
| Hand-rolled `WithDiffHook` per project | Works, but every project re-writes the same change-filtering boilerplate. |

A migration `DiffHook` is the only approach that keeps **code-generation and FK edges intact**. `entskiptable` packages it once, generically, with composable predicates.

## Quick start

Mark which tables are externally-owned when you run auto-migration:

```go
import (
	"entgo.io/ent/dialect/sql/schema"
	"github.com/khanakia/entx/entskiptable"
)

err := client.Schema.Create(ctx,
	schema.WithForeignKeys(false),
	schema.WithDiffHook(entskiptable.SkipHook(
		entskiptable.Any(
			entskiptable.ByPrefix("auth_"),          // a whole module-owned namespace
			entskiptable.ByName("billing_accounts"), // a specific foreign table
		),
	)),
)
```

ent still **reads** `auth_*` and `billing_accounts` (queries, edges, eager-loading all work) but **never emits DDL** for them.

## How it works

```
client.Schema.Create(ctx, WithDiffHook(entskiptable.SkipHook(pred)))
        │
        ▼
ent computes the schema diff   →   AddTable / ModifyTable / DropTable / RenameTable …
        │
        ▼
entskiptable drops every change whose target table matches `pred`
        │
        ▼
0 DDL emitted for excluded tables  (reads still work)
```

The hook wraps ent's differ: it lets ent compute the migration plan as usual, then removes any table-scoped change targeting an excluded table before it is applied. Non-table changes always pass through.

## API

```go
// Predicate reports whether a table must be excluded from migration.
type Predicate func(table string) bool

// ByName excludes the exact table names given.
func ByName(names ...string) Predicate

// ByPrefix excludes any table whose name starts with one of the
// prefixes (e.g. an entire module-owned namespace like "auth_").
func ByPrefix(prefixes ...string) Predicate

// Any combines predicates with OR (nil predicates are ignored).
func Any(preds ...Predicate) Predicate

// SkipHook returns an ent migration DiffHook that drops every schema
// change whose target table matches skip. A nil skip is a no-op.
func SkipHook(skip Predicate) schema.DiffHook
```

`Predicate` is just `func(string) bool`, so you can supply your own logic:

```go
schema.WithDiffHook(entskiptable.SkipHook(func(table string) bool {
	return table == "legacy_users" || strings.HasSuffix(table, "_readonly")
}))
```

## Compatibility

| | Version |
| --- | --- |
| Go | 1.26+ |
| [entgo.io/ent](https://entgo.io) | v0.14.x |
| [ariga.io/atlas](https://atlasgo.io) | v1.2.x |

Only imports `entgo.io/ent/dialect/sql/schema` and `ariga.io/atlas/sql/schema` — no other runtime dependencies.

## FAQ

### Does this disable migrations entirely?

No. Only changes targeting tables your predicate matches are dropped. Every other table migrates normally.

### Will it stop me reading the excluded table?

No. The hook operates purely on the migration diff. Queries, edges, and eager-loading on the excluded table keep working.

### Why not `entsql.Annotation{Skip: true}`?

It breaks ent code-generation as soon as another schema has an edge to the skipped type. `entskiptable` works regardless of edges.

### Is it safe with `WithForeignKeys(false)`?

Yes — that's the recommended setup. The hook is independent of FK handling; it only filters table-level changes.

### Does it support `RenameTable`?

Yes. `AddTable`, `DropTable`, `ModifyTable`, and `RenameTable` are all filtered when their target table matches.

### How do I add another foreign table later?

Add it to the predicate: `entskiptable.Any(entskiptable.ByPrefix("auth_"), entskiptable.ByName("foo", "bar"))`. One place, exact match.

## License

Part of the [entx](https://github.com/khanakia/entx) toolkit for the ent ORM.

<sub>Keywords: ent ORM migration, entgo skip table, exclude table from ent auto-migration, ent DiffHook, ent read-only table, externally-owned table ent, ent multi-module shared database, prevent ent ALTER TABLE, Go ent Atlas migration hook.</sub>
