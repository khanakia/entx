# entxmigrate — skip / exclude tables from [ent](https://entgo.io) auto-migration (Go ORM)

> **Exclude externally-owned tables from ent's auto-migration.** A tiny, dependency-light migration `DiffHook` for the [ent ORM](https://entgo.io) that stops `client.Schema.Create` from emitting `CREATE` / `ALTER` / `DROP` DDL against tables your ent client only *reads* — tables owned by another service, another ent client, a database view, or a hand-managed migration. Keep ent code-generation and foreign-key edges fully intact.

`go get github.com/khanakia/entx/entxmigrate`

[Quickstart](#quickstart) · [Install](#install) · [Why](#why-entxmigrate) · [API](#api) · [How do I…](#how-do-i) · [FAQ](#faq)

---

## What is entxmigrate?

`entxmigrate` solves a common multi-module / micro-service problem with the [ent ORM](https://entgo.io): you model a table as a normal `ent.Schema` so you can **query it and traverse edges to it**, but that table is actually **owned by something else** — a different ent client, a separate service, a database view, or a manually-managed table. ent's auto-migration doesn't know that. On the next `client.Schema.Create()` it will happily reshape the foreign table to match your local schema, e.g.:

```sql
ALTER TABLE "auth_users" ALTER COLUMN "email" DROP NOT NULL, ALTER COLUMN "status" SET DEFAULT 'available';
```

That is destructive, cross-owner DDL on a table you don't own.

`entxmigrate` is a small [ent migration `DiffHook`](https://entgo.io/docs/migration#diff-hook) that **filters the computed schema changes**: any `AddTable` / `DropTable` / `ModifyTable` / `RenameTable` targeting a table you mark as externally-owned is dropped before ent applies it. Reads still work — only schema **changes** are suppressed.

```
schema author models the foreign table   →   ent diffs it   →   entxmigrate strips its changes   →   0 DDL emitted
       (so edges + queries work)              (wants ALTER)        (DiffHook)                          (table untouched)
```

No code generation. No struct tags. No fork of ent. Just one hook in your migration call.

## Why entxmigrate?

ent's built-in escape hatches don't cover this case:

| Approach | Problem |
|---|---|
| `entsql.Annotation{Skip: true}` | Breaks code-generation when **other tables hold edges** to the skipped type (the migration template nil-dereferences). |
| `ent.View` schema | A view-typed ent schema **cannot carry foreign-key edges** (ent ≤ v0.14.x) — you lose the relationships you modeled the table for. |
| Hand-rolled `WithDiffHook` per project | Works, but every project re-writes the same change-filtering boilerplate. |

A migration `DiffHook` is the only approach that keeps **code-generation and FK edges intact**. `entxmigrate` packages it once, generically, with composable predicates.

## Quickstart

Mark which tables are externally-owned when you run auto-migration:

```go
import (
    "entgo.io/ent/dialect/sql/schema"
    "github.com/khanakia/entx/entxmigrate"
)

err := client.Schema.Create(ctx,
    schema.WithForeignKeys(false),
    schema.WithDiffHook(entxmigrate.SkipHook(
        entxmigrate.Any(
            entxmigrate.ByPrefix("auth_"),          // a whole module-owned namespace
            entxmigrate.ByName("billing_accounts"), // a specific foreign table
        ),
    )),
)
```

That's it. ent will still **read** `auth_*` and `billing_accounts` (queries, edges, eager-loading all work) but will **never emit DDL** for them.

## Install

```bash
go get github.com/khanakia/entx/entxmigrate
```

**Compatibility**

| | Version |
|---|---|
| Go | 1.26+ |
| [entgo.io/ent](https://entgo.io) | v0.14.x |
| [ariga.io/atlas](https://atlasgo.io) | v1.2.x |

`entxmigrate` only imports `entgo.io/ent/dialect/sql/schema` and `ariga.io/atlas/sql/schema` — no other runtime dependencies.

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
// change whose target table matches skip. nil skip == no-op.
func SkipHook(skip Predicate) schema.DiffHook
```

`Predicate` is just `func(string) bool`, so you can supply your own logic:

```go
schema.WithDiffHook(entxmigrate.SkipHook(func(table string) bool {
    return table == "legacy_users" || strings.HasSuffix(table, "_readonly")
}))
```

## How do I…

**…protect an entire namespace owned by another module?**
`entxmigrate.ByPrefix("auth_")` — every `auth_*` table is left untouched.

**…add one more foreign table later?**
Add it to the predicate: `entxmigrate.Any(entxmigrate.ByPrefix("auth_"), entxmigrate.ByName("foo", "bar"))`. One place, exact match.

**…keep reading the table?**
You already can. `SkipHook` only filters schema *changes*; `SELECT` / edge traversal / eager-loading are unaffected.

**…use it with versioned migrations / Atlas?**
`SkipHook` returns a standard `schema.DiffHook`, so it composes with any `schema.With*` option you already pass to `client.Schema.Create` or your Atlas-based migration generation.

## FAQ

**Does this disable migrations entirely?**
No. Only changes targeting tables your predicate matches are dropped. Every other table migrates normally.

**Will it stop me reading the excluded table?**
No. The hook operates purely on the migration diff. Queries, edges and eager-loading on the excluded table keep working.

**Why not `entsql.Annotation{Skip: true}`?**
It breaks ent code-generation as soon as another schema has an edge to the skipped type. `entxmigrate` works regardless of edges.

**Is it safe with `WithForeignKeys(false)`?**
Yes — that's the recommended setup. The hook is independent of FK handling; it only filters table-level changes.

**Does it support `RenameTable`?**
Yes. `AddTable`, `DropTable`, `ModifyTable`, and `RenameTable` are all filtered when their target table matches.

## License

Part of the [entx](https://github.com/khanakia/entx) toolkit for the ent ORM.

---

<sub>Keywords: ent ORM migration, entgo skip table, exclude table from ent auto-migration, ent DiffHook, ent read-only table, externally-owned table ent, ent multi-module shared database, prevent ent ALTER TABLE, Go ent Atlas migration hook.</sub>
