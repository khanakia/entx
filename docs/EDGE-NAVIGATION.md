# Edge navigation

How enttui turns ent's edge graph into a navigable UI.

## Two edge kinds

| Kind         | When                       | UI behavior |
|--------------|----------------------------|-------------|
| `EdgeUpward` | `edge.X(...).Unique()`     | Pressing the trigger jumps to the parent row's preview. Adds a page to the back-stack. |
| `EdgeDrill`  | non-unique (1→N edge)      | Pressing the trigger opens a new Browser scoped to the target rows. Back-stack records the parent. |

In both cases the generator emits a typed resolver closure into the generated `register_<name>.go`:

```go
// Upward
ResolveUpward: func(ctx context.Context, r *ent.Post) (runtime.EntityRef, error) {
    tgt, err := client.Post.QueryAuthor(r).Only(ctx)
    if err != nil { return runtime.EntityRef{}, err }
    return runtime.EntityRef{Kind: "author", ID: tgt.ID}, nil
},

// Drill
ResolveDrill: func(ctx context.Context, r *ent.Post) (runtime.EntityRefList, error) {
    ids, err := client.Post.QueryComments(r).IDs(ctx)
    if err != nil { return runtime.EntityRefList{}, err }
    return runtime.EntityRefList{Kind: "comment", IDs: ids}, nil
},
```

No reflection, no any-type erasure inside the closure body — fully typed against the concrete `*ent.<Type>`.

## Trigger keys

Each edge has a single letter key that follows it. Two ways triggers are assigned:

1. **By annotation** (M1): `enttui.Upward("a")` / `enttui.Drill("c")`.
2. **By convention**: the generator walks the edge name letter by letter, skipping reserved keys (`h`, `j`, `k`, `l`, `g`, `n`, `p`, `r`, `q`, `v`, `y`, `s` — vim-faithful top-level), takes the first unused one. **`enter` is reserved for "open preview" — never auto-assigned to an edge.**

The preview footer always shows the chosen triggers so users discover them:

```
── edges ──
  a      → Authors
  t      → Tags
  c      Comments
  r      Replies
```

## Following an edge

Pressing a trigger calls `b.followEdge(e)` in the browser:

1. Get the current selected row.
2. Open a 5-second context derived from `app.ctx`.
3. Call the typed resolver closure.
4. On success:
   - Upward → `app.pushBrowser(targetKind, targetID)` opens a new page showing the target list with selection moved to that ID.
   - Drill → `app.pushBrowserList(EntityRefList{Kind, IDs})` opens a new page restricted to those IDs.
5. On error: status bar shows `[red]edge error: …[-]`, no navigation happens.

## Back-stack

The `App.stack` is a slice of pageEntry structs. Push on every `pushBrowser` / `pushBrowserList`. Pop with `esc`:

```go
func (a *App) popPage() {
    if len(a.stack) <= 1 { return }
    top := a.stack[len(a.stack)-1]
    a.pages.RemovePage(top.name)
    a.stack = a.stack[:len(a.stack)-1]
    prev := a.stack[len(a.stack)-1]
    a.pages.SwitchToPage(prev.name)
}
```

The first page (initial mount) can't be popped — `esc` is a no-op there. Press `q` to quit instead.

## Drill: how the ID-filter works

When `pushBrowserList(refs)` runs, the new browser calls `b.setIDFilter(refs.IDs)`:

```go
func (b *browser) setIDFilter(ids []string) {
    b.idFilter = make(map[string]bool, len(ids))
    for _, id := range ids { b.idFilter[id] = true }
    b.refresh()
}
```

On refresh, the **full Fetch is still executed** (with whatever project / filter / sort options apply), then the result is post-filtered in memory against `idFilter`. This is intentional for v1:

- Keeps Fetch closures schema-agnostic (no `IDIn(...)` predicates baked in).
- Works correctly with sort + paginate semantics.
- 200-row page size makes the in-memory filter cost negligible.

If you need to drill into millions of rows, this is the first place to optimize. The trade-off is generating per-entity `FetchByIDs` closures or pushing a `WhereIDIn` predicate through `ListOpts` — both possible, both deferred to M2.

## Cross-type edges & registration order

For an edge from `Post` to `Comment` to resolve, both types must be registered. The generator only emits an edge if its target type is itself browsable (i.e. the target is going to be registered). If the target gets filtered out (e.g. you `--skip Comment`), the edge from Post is silently dropped — your Post UI just won't have that footer entry.

## Polymorphic edges

ent has no native polymorphism. If your schema uses `entity_table + entity_id` columns (a common pattern for `Comment` / `Reaction` / `Audit` tables that can attach to many entity types), there's no `Edge` to emit and the generator can't pick it up. Solutions for v1:

- Add a hand-written `register_*.go` for the parent type's edges (and `--skip` its codegen).
- Wait for M2 annotation `enttui.PolyEdge(...)` which will read a predicate-builder closure and emit drill behavior.

## Reserved triggers — full list

| Key   | Why reserved |
|-------|--------------|
| `h` `j` `k` `l` | Vim navigation (h=left, j=down, k=up, l=right) |
| `g`   | `gg` first-row prefix |
| `n` / `p` | Next / previous page |
| `r`   | Refresh |
| `q`   | Quit |
| `v`   | Visual-range select |
| `y`   | Yank operator (`yy`/`yc`/`yj`/`yv`) |
| `s`   | Reserved (sort lives under `,s`) |

`,` is the leader and is never an edge trigger. Single-letter edge triggers can use any other a-z char. Multi-char triggers (`gg`, `dd`, etc.) are not supported; one keystroke per edge.

## Related docs

- [RUNTIME.md](RUNTIME.md) — back-stack + page management internals.
- [ANNOTATIONS.md](ANNOTATIONS.md) — `Upward(...)` / `Drill(...)` annotation reference.
- [CONVENTIONS.md](CONVENTIONS.md) — automatic trigger assignment rules.
