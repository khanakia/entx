# entreadonly

[![Go Reference](https://pkg.go.dev/badge/github.com/khanakia/entx/entreadonly.svg)](https://pkg.go.dev/github.com/khanakia/entx/entreadonly)
[![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8)](https://go.dev)
[![ent v0.14.x](https://img.shields.io/badge/ent-v0.14.x-7e3aed)](https://entgo.io)

**Make an [ent](https://entgo.io) schema read-only at code-generation time.** `entreadonly` strips the `Create` / `Update` / `Delete` builders and client write methods for any annotated entity, so writing it **fails to compile** — not at runtime. The schema stays a normal table: queries, edges, and GraphQL ([entgql](https://entgo.io/docs/graphql)) are fully intact.

```bash
go get github.com/khanakia/entx/entreadonly
```

## What & why

You model a table as an `ent.Schema` so you can **query it and traverse edges to it**, but it must never be **written** through this client — it's owned by another service, projected from another source of truth, or simply immutable by design (audit logs, identity tables, materialized projections).

ent normally generates a full write surface for every schema:

```go
client.User.Create()...Save(ctx)   // you do NOT want this to exist
client.User.UpdateOneID(id)...Save(ctx)
client.User.DeleteOneID(id)
```

A runtime hook can *reject* those calls, but the methods still exist and compile — a teammate can still write them and only find out at runtime. `entreadonly` removes the methods entirely, so the guarantee is **enforced by the compiler**.

ent has no built-in switch for this:

| Approach | Problem |
| --- | --- |
| `ent.View` | The only built-in read-only mode, but a view **cannot carry foreign-key edges** and breaks entgql's node codegen (ent ≤ v0.14.x). Unusable for edge-rich schemas. |
| Runtime hook / privacy rule | Rejects writes only at **runtime** — the builders still compile and ship. |
| Forking ent's create/update/delete templates | Works once, but breaks on every ent upgrade. |

`entreadonly` keeps the schema a normal table and removes only the write surface — generically, driven by one annotation.

## Real-world use case: a dedicated auth module

This is the scenario `entreadonly` was built for.

You split authentication into its own module — call it `authmgr`. It has its **own ent schema, its own ent client, and it owns the `auth_users` table**: password hashing, email verification, RBAC, session lifecycle. `authmgr` is the *single source of truth* for identity.

Your main application (`app`) also has an ent client and a huge relational graph: `Project`, `Task`, `TaskAssignee`, `ProjectMember`, `Comment`, … and most of those have an edge to a user. To make `project.QueryMembers().QueryUser()`, `task.assignee.user`, GraphQL `user { name }`, and a `/users` page work, `app` **must model `User` as an `ent.Schema`** mapped onto the same `auth_users` table:

```go
// app/ent/schema/user.go — a projection of authmgr's table
func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "auth_users"}, // owned by authmgr
	}
}
```

### "Why copy the schema at all? Why not just import authmgr's `User`, or do it dynamically?"

The honest answer: **ent + Go give you no choice.** This is a structural constraint, not bad design.

- **ent edges are intra-graph and code-generated, not runtime.** An ent edge (`Task` → `User`) is resolved by code that `entc` generates from *one* schema graph in *one* `entc.Generate` run. `app`'s `Task` edge cannot point at `authmgr/ent.User` — that's a *different* generated package, a *different* `*ent.Client`, with no shared builders, no shared query planner, no shared eager-loader. ent has no concept of a cross-module / cross-client edge. For `task.assignee.user` (and its `WITH`/eager-load, and the entgql `User` node) to exist in `app`, a `User` schema node must exist in **`app`'s own graph**.
- **You can't "be dynamic."** Go is statically typed and ent is *code-generated ahead of time* — the query builders, edge loaders, predicates, and the entgql GraphQL types are all concrete Go produced at build time from the schema. There is no reflection-based "just resolve this id against another service's client at runtime" that gives you typed edges, eager-loading, and a GraphQL `User` type. A dynamic lookup (`go fetch user by id from authmgr`) is possible, but it is *not* an ent edge: you lose `.WithAssignee()`, N+1-safe batching, entgql connections, filtering/ordering by user fields, and the `/users` page — i.e. everything you modeled it for.
- **Nothing is duplicated except the type.** `authmgr` and `app` share one physical database. The `app` `User` schema is **not a second copy of the data** — it's a second *typed view* of the same `auth_users` rows, required only because each Go module gets its own generated ent client. Zero rows are copied; only a struct + builders are generated.

So the projection is unavoidable. What *is* avoidable is the accidental write surface it drags in — and that's the entire job of `entreadonly`.

Now you have a problem. ent generated a full write surface on the `app` client:

```go
app.User.Create().SetEmail("x@y.z").Save(ctx)        // bypasses password hashing
app.User.UpdateOneID(id).SetPassword("plain").Save() // bypasses validation, no audit
app.User.DeleteOneID(id).Exec(ctx)                   // deletes an identity authmgr owns
```

Every one of those **compiles**. Nothing stops a teammate (or an AI assistant, or a copy-pasted resolver) from writing identity data through the wrong module — silently corrupting the source of truth, skipping `authmgr`'s hashing/verification/RBAC, and leaving no audit trail. A code review might catch it. It also might not.

There is no clean ent answer:

- `ent.View` would make it read-only — but `User` is the target of four foreign-key edges (`TaskAssignee`, `ProjectMember`, …) and ent ≤ v0.14.x can't put FK edges on a view (codegen panics), and entgql can't build a Node for a view either. You'd lose the very edges you modeled `User` for.
- A runtime privacy hook rejects the write — but only when it runs in production. The method still exists, still compiles, still ships.

**With `entreadonly`, the `app` side simply cannot write identity:**

```go
func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "auth_users"},
		entreadonly.ReadOnly(), // app can read users; only authmgr writes them
	}
}
```

`app.User.Create()` / `UpdateOne()` / `DeleteOneID()` **no longer exist** — the mistake is a compile error, caught on every build and in every editor, before it can ever reach a database. Meanwhile `app.User.Query()`, every edge traversal, and the GraphQL `user` field keep working exactly as before. `authmgr`, with its own client and its own (untouched) write builders, remains the sole writer.

One annotation turns "we *trust* nobody writes the foreign table" into "the compiler *guarantees* nobody can."

## Quick start

**1. Annotate the schema:**

```go
import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"github.com/khanakia/entx/entreadonly"
)

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entreadonly.ReadOnly(), // ← compile-time read-only
	}
}
```

**2. Register the extension in your code-gen entry point:**

```go
import "github.com/khanakia/entx/entreadonly"

err := entc.Generate("./schema", &gen.Config{
	Target:  "./ent",
	Package: "yourapp/ent",
}, entc.Extensions(
	entreadonly.NewExtension(),
))
```

**3. Run the strip after generation** (one extra line in your codegen task):

```go
// after entc.Generate(...)
if err := entreadonly.Strip("./ent"); err != nil {
	log.Fatal(err)
}
```

Now `client.User.Create()`, `UpdateOne()`, `DeleteOneID()` and `row.Update()` **do not exist** — any write is a compile error. `client.User.Query()` and every edge traversal still work.

## How it works

The split is deliberate — it's the only approach that is both generic and stable across ent releases:

```
schema: entreadonly.ReadOnly()
        │
        ▼
entc Extension (gen.Hook, codegen-time)
        │  scans the graph for the annotation
        ▼
ent/entreadonly_manifest.json   ["User", ...]
        │
        ▼
entreadonly.Strip("./ent") (post-codegen, AST)
        │  keyed only on type names
        ▼
write surface removed → writes fail to COMPILE
```

1. **Extension** — a `gen.Hook` runs during `entc.Generate`, finds every node carrying the annotation, and writes a small JSON manifest of type names. Generic: nothing is hardcoded.
2. **Strip** — a deterministic AST pass reads the manifest and, for each type, deletes its `*_create.go` / `*_update.go` / `*_delete.go` builder files, removes the `<T>Client` write methods, neutralises the generic `<T>Client.mutate` dispatcher (kept so the global `Client.Mutate` switch still compiles, but it returns a read-only error), and removes the `(*<T>).Update` convenience.

The strip works on the **stable shape of generated output** (type names), not on ent's internal templates, so it survives ent upgrades far better than a template fork.

## API

```go
// ReadOnly is the schema annotation that marks a schema read-only.
// Add it to a schema's Annotations().
func ReadOnly() schema.Annotation

// NewExtension returns the entc extension. Register it via
// entc.Extensions(entreadonly.NewExtension()).
func NewExtension() *Extension

// Strip removes the write surface for every annotated type, using the
// manifest written by the extension. Call it after entc.Generate.
// Idempotent; a no-op when nothing is annotated.
func Strip(genDir string) error
```

## Compatibility

| | Version |
| --- | --- |
| Go | 1.26+ |
| [entgo.io/ent](https://entgo.io) | v0.14.x |

Only depends on `entgo.io/ent` (`entc`, `entc/gen`, `schema`) plus the Go standard library `go/ast`.

## FAQ

### Does it disable queries too?

No. Only the **write** surface is removed. `Query()`, `Get()`, `GetX()`, edge traversals, and eager-loading are untouched.

### Why not `ent.View`?

`ent.View` is read-only but cannot carry foreign-key edges and breaks entgql node code-generation in ent ≤ v0.14.x. `entreadonly` keeps the schema a normal table, so edges and GraphQL keep working.

### The strip edits generated files — is that safe?

It is deterministic and idempotent, and it must run **on every code-generation**. Chain it right after `entc.Generate` in the same task. If generation runs without the strip, the write surface reappears until the strip runs again — your build is the safety net (any code calling the removed methods won't compile).

### Can I keep a runtime guard as well?

Yes. `entreadonly` composes with a schema `Hooks()` that rejects mutations — useful as defence-in-depth for any non-generated code path.

### How do I make another schema read-only?

Add `entreadonly.ReadOnly()` to its `Annotations()`. Nothing else — the extension discovers it automatically.

## License

Part of the [entx](https://github.com/khanakia/entx) toolkit for the ent ORM.

<sub>Keywords: ent ORM read-only entity, entgo disable create update delete, compile-time read-only ent schema, ent immutable model Go, prevent ent mutations, ent projection table, ent codegen strip builders, entgo read-only without ent.View, ent multi-module identity table.</sub>
