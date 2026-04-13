# Why entcascade?

**ent disables foreign keys by default in most real deployments** (`WithForeignKeys(false)`). That means your database won't cascade deletes for you — every time you delete a parent, you have to:

1. Delete each child table in the right order
2. Clean up M2M junction rows
3. Handle soft-delete semantics where applicable
4. Preserve rows that should survive with a NULL FK
5. Wrap everything in a transaction
6. Remember to update the code whenever the schema changes

That's a lot of boilerplate, and it silently drifts out of sync with your schema every time someone adds an edge. `entcascade` generates all of it from your schema annotations.

This document walks through every edge case entcascade handles and what it would cost you to do yourself.

---

## The headline example

```go
// schema/chatbot.go
func (Chatbot) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}
```

After `go generate ./...`:

```go
// Generated. Deletes chatbot + posts + comments + post_tags + profile,
// in the right order, in a transaction.
ent.CascadeDeleteChatbot(ctx, client, chatbotID)
```

That's it. Add an edge to the schema, regenerate, and the new child is included automatically. No manual delete order to maintain.

---

## Use cases, one by one

### 1. Default: hard cascade

**Problem.** Deleting a parent should remove all its children. Doing this by hand means writing delete statements for every child edge, in dependency order, and remembering to update the code whenever you add a new edge.

**entcascade.** Annotate the parent with `Cascade()`. Every O2M / O2O edge is walked automatically. Add a new edge? Regenerate. Done.

```go
entcascade.Cascade()
// Chatbot -> Posts -> Comments + PostTags -> automatically cascaded
```

---

### 2. Unlink instead of delete (`WithUnlink`)

**Problem.** You want to delete a `Category`, but the posts categorized under it should survive with `category_id = NULL`. Writing this by hand means a careful `UPDATE ... SET category_id = NULL WHERE category_id = ?` *before* the delete, and making sure every other column is preserved.

**entcascade.**

```go
entcascade.Cascade(entcascade.WithUnlink("posts"))
```

Generated code emits `client.Post.Update().Where(post.CategoryID(id)).ClearCategoryID().Exec(ctx)`. Other columns untouched.

---

### 3. Soft delete, auto-detected

**Problem.** Many schemas have a `deleted_at` field that marks a row as archived instead of physically removing it. Manual cascades have to know which children use this convention and which don't, and call `UPDATE SET deleted_at = now()` on the soft-deleted ones while hard-deleting the rest.

**entcascade.** If a child type has a `deleted_at` field, it's auto-detected and soft-deleted during cascade. Zero config:

```go
// Article has Cascade(), Revision has a deleted_at field.
// Generated code: UPDATE revisions SET deleted_at = NOW() WHERE article_id = ?
```

---

### 4. Soft delete with a non-standard column (`WithSoftDelete`)

**Problem.** Your "deleted" column is named `archived_at` or `removed_at` — not `deleted_at`. Auto-detect won't see it.

**entcascade.** Opt in explicitly:

```go
entcascade.Cascade(entcascade.WithSoftDelete("notes", "archived_at"))
```

---

### 5. Force hard delete (`WithHardDelete`)

**Problem.** A child happens to have a `deleted_at` column, but for *this* edge you actually want to hard-delete. Auto-detect would soft-delete incorrectly.

**entcascade.** Override the auto-detection:

```go
entcascade.Cascade(entcascade.WithHardDelete("draft_versions"))
```

---

### 6. Skip an edge entirely (`SkipEdges`)

**Problem.** A `Team` has an `owner` edge pointing at a `User`, but the user is shared and you never want to delete them when deleting the team. Manual cascades have to remember the exception every time.

**entcascade.**

```go
entcascade.Cascade(entcascade.SkipEdges("owner"))
```

---

### 7. Nested cascades (depth > 1)

**Problem.** Deleting a `User` cascades to their `Posts`; each post has `Comments` and `PostTags`. A manual implementation has to walk the tree depth-first, query intermediate IDs, and issue `DELETE ... WHERE parent_id IN (?,?,...)` for the grandchildren.

**entcascade.** Handled automatically, including the intermediate ID queries and the `IN(...)` batching.

---

### 8. **Nested annotations on intermediate types** (regression fixed in 0.2)

**Problem.** Before the fix, `entcascade` only read the root type's annotation. If an intermediate type carried `WithUnlink` or `WithSoftDelete`, that rule was silently ignored when a parent cascaded *through* it — leading to quiet data loss.

Example bug:

```go
Chatbot.Cascade()
Folder.Cascade(WithUnlink("channels"))

// BEFORE: CascadeDeleteChatbot walked Folder.channels and HARD-DELETED them.
// AFTER:  CascadeDeleteChatbot unlinks channels (folder_id = NULL).
```

**entcascade** now reads each intermediate's annotation during the walk. Your `WithUnlink` / `WithSoftDelete` / `SkipEdges` / `WithHardDelete` rules are honored at every depth. Covered by `TestCascadeDeleteWorkspace_NestedUnlink`, `..._NestedSoftDelete`.

---

### 9. M2M junction table cleanup

**Problem.** A `Post` has a M2M `tags` edge through a `PostTag` join table. Deleting a post should delete its rows in `post_tags` but leave the `Tag` rows alone (they're shared). Manual cascades have to know the join table name, its FK column, and issue the correct `DELETE FROM post_tags WHERE post_id = ?`.

**entcascade.** Detects `Through(...)` edges and emits the junction cleanup automatically. Standalone tags survive.

---

### 10. Atomic transactions

**Problem.** Partial cascades are worse than no cascades — if step 3 of 5 fails, you have dangling orphan rows. Manual implementations have to remember to wrap everything in `client.Tx(ctx)` and handle rollback on *every* error path.

**entcascade.** Every cascade function uses a transaction. Any error rolls back everything. You don't have to think about it.

---

### 11. **Nested-tx composition (`tx.Client()`)** (added in 0.2)

**Problem.** You want to compose multiple cascades in one atomic block:

```go
tx, _ := client.Tx(ctx)
CascadeDeleteChannelBatch(ctx, tx.Client(), channelIDs)  // nope
CascadeDeleteFolder(ctx, tx.Client(), folderID)          // ent.ErrTxStarted
tx.Commit()
```

ent refuses nested transactions; the cascade fails with `ErrTxStarted`.

**entcascade.** Detects `tx.Client()` and *reuses* the outer transaction. All your cascades share one DB transaction; the caller owns commit/rollback:

```go
tx, _ := client.Tx(ctx)
ent.CascadeDeleteChannelBatch(ctx, tx.Client(), channelIDs)
ent.CascadeDeleteFolder(ctx, tx.Client(), folderID)
// any extra tx.Client() operations...
tx.Commit()  // or tx.Rollback() — undoes everything
```

Covered by `TestCascadeNestedTx_NoErrTxStarted`, `..._ComposeMultiple`, `..._OuterRollbackUndoesCascade`.

---

### 12. Pre / Post hooks inside the transaction

**Problem.** You need to run business logic — validation, audit logging, decrementing a billing counter — as part of the cascade. If you run it before calling the cascade, it's in a different transaction than the delete (bad). If you run it after, the cascade has already committed (bad).

**entcascade.** `WithHooks` puts your code inside the cascade transaction:

```go
ent.CascadeDeleteUserWithHooks(ctx, client, id, ent.CascadeDeleteUserHooks{
    Pre:  validateDeletionAllowed,
    Post: decrementTenantCount,
})
```

Pre-hook error aborts; post-hook error rolls back the already-executed deletes.

---

### 13. Batch delete with a single transaction

**Problem.** Deleting 500 users one at a time is 500 transactions. Manual batching means adapting every predicate from `= ?` to `IN (?,?,...)` and re-wrapping.

**entcascade.**

```go
ent.CascadeDeleteUserBatch(ctx, client, userIDs)
```

One transaction. `IN (...)` predicates throughout the generated code. Empty slice is a zero-query no-op (no edge-case crashes).

---

### 14. Idempotent deletes

**Problem.** If two workers both try to delete the same resource, the second one blows up with "not found". Manual implementations have to use `IGNORE NOT FOUND` semantics or catch errors.

**entcascade.** Uses `Delete().Where(id.EQ(x))` rather than `DeleteOneID(x)`. Matching zero rows is not an error. Calling `CascadeDeleteUser` twice just succeeds the second time.

---

### 15. Non-existent IDs are safe

**Problem.** Cascading on an ID that was never created (race condition, user-supplied input, etc.) must not explode.

**entcascade.** Zero-row WHERE clauses return cleanly. `TestCascadeDeleteUser_NonexistentID` proves it.

---

### 16. Isolation between siblings

**Problem.** Off-by-one in a `WHERE` clause and you've just deleted everyone else's data. Manual cascades have to be carefully reviewed for predicate correctness.

**entcascade.** Every generated predicate is scoped to the cascade's ID (or batch of IDs). `TestCascadeDeleteUser_Isolation` asserts that deleting user A leaves user B's tree fully intact.

---

### 17. Unlink preserves other columns

**Problem.** A hand-written `UPDATE posts SET category_id = NULL` is one misplaced column away from clobbering the title or body.

**entcascade.** Generated unlinks use ent's type-safe `.ClearCategoryID()` — no other columns are touched. `TestCascadeDeleteCategory_UnlinkPreservesData` verifies.

---

### 18. Empty children / orphan parents

**Problem.** Cascading a parent that has no dependents should still delete the parent cleanly. Generated code has to guard against "zero rows to cascade into" without short-circuiting the parent delete.

**entcascade.** `if len(childIDs) > 0 { ... }` guards are emitted around nested cascades. The parent delete still runs.

---

## Summary table

| You'd write by hand | entcascade does for you |
| --- | --- |
| Delete order for every edge | Generated from schema |
| M2M junction cleanup | Generated with `Through()` detection |
| Soft vs hard delete per edge | Auto-detect `deleted_at` + explicit overrides |
| `SET NULL` on unlink edges | `WithUnlink(...)` — one line |
| Skip non-owned edges | `SkipEdges(...)` — one line |
| Transactional safety | Every cascade is a tx |
| Composition across cascades | `tx.Client()` reuses your tx |
| Pre/Post hooks in-tx | `CascadeDeleteXWithHooks` |
| Batch API | `CascadeDeleteXBatch` |
| Idempotent re-runs | WHERE-based, not GetOne |
| Nested-annotation correctness | Respects intermediate type rules |
| Keeping code in sync with schema | Just regenerate |

## When you should NOT use entcascade

- **You have database-level FKs with `ON DELETE CASCADE`.** Your DB already does this; entcascade is redundant.
- **You're working in a CQRS / event-sourced setup** where "delete" is actually a domain event, not a row removal.
- **You want physical-delete semantics enforced by the DB** regardless of application bugs — FKs are stronger than app-level cascades.

For every other ent project with `WithForeignKeys(false)`, entcascade replaces hundreds of lines of hand-written, schema-drift-prone code with annotations.

## Try it

See [README.md](./README.md) for setup, [CHANGELOG.md](./CHANGELOG.md) for release history, and [../testent/TESTS.md](../testent/TESTS.md) for the 33 integration tests that guard every use case listed above.
