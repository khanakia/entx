# entcascade

An [ent](https://entgo.io) codegen extension that generates type-safe cascade delete functions from schema annotations.

## Why disable foreign keys?

Many ent projects run with `WithForeignKeys(false)` in production. Here's why:

**Faster migrations.** Adding or altering foreign keys on large tables requires expensive locks. Without FKs, `CREATE TABLE` and `ALTER TABLE` are instant — no full table scans, no waiting for row-level validation. In production with millions of rows, this is the difference between a zero-downtime migration and a minutes-long lock.

**Flexible schema evolution.** FKs create ordering dependencies — you can't drop a table that other tables reference, can't easily rename columns, can't do circular references. Without them, you can migrate tables independently and in any order.

**Cross-database portability.** Some databases (like CockroachDB, certain MySQL configs, or SQLite in default mode) have limited or quirky FK support. Disabling FKs at the ent level means your schema works consistently everywhere.

**ent recommends it.** The ent documentation itself [notes](https://entgo.io/docs/migrate/#foreign-keys) that many users disable foreign keys. The framework validates relationships at the application level through its generated code — the database FKs are redundant for correctness.

**The trade-off:** without database-level `ON DELETE CASCADE`, deleting a parent entity leaves orphaned children. You need application-level cascade deletes — and that's exactly what `entcascade` generates for you.

## The problem without entcascade

Without this extension, you write manual delete functions for every entity:

```go
func DeleteUserAndDependents(ctx context.Context, client *ent.Client, userID int) error {
    tx, err := client.Tx(ctx)
    if err != nil {
        return err
    }

    // Query all post IDs first (needed for nested deletes)
    postIDs, err := tx.Post.Query().Where(post.AuthorID(userID)).IDs(ctx)
    if err != nil {
        return rollback(tx, err)
    }

    if len(postIDs) > 0 {
        // Delete comments on those posts
        if _, err := tx.Comment.Delete().Where(comment.PostIDIn(postIDs...)).Exec(ctx); err != nil {
            return rollback(tx, err)
        }
        // Delete post_tag junction rows
        if _, err := tx.PostTag.Delete().Where(posttag.PostIDIn(postIDs...)).Exec(ctx); err != nil {
            return rollback(tx, err)
        }
        // Delete posts
        if _, err := tx.Post.Delete().Where(post.AuthorID(userID)).Exec(ctx); err != nil {
            return rollback(tx, err)
        }
    }

    // Delete profile
    if _, err := tx.Profile.Delete().Where(profile.UserID(userID)).Exec(ctx); err != nil {
        return rollback(tx, err)
    }

    // Delete user
    if _, err := tx.User.Delete().Where(user.ID(userID)).Exec(ctx); err != nil {
        return rollback(tx, err)
    }

    return tx.Commit()
}
```

This is ~40 lines for one entity. Now multiply by every entity that needs cascade deletes. And when you add a new edge to the schema, you have to remember to update the manual function — or you get orphaned rows.

## The solution

One annotation, zero manual code:

```go
func (User) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}
```

```go
// Generated — handles the entire tree in a transaction.
err := ent.CascadeDeleteUser(ctx, client, userID)
```

The extension inspects edges at codegen time and generates the same delete logic you'd write by hand — but it's always correct, always in sync with the schema, and handles nested cascades automatically.

## Use cases

**SaaS tenant deletion.** Delete a workspace and everything inside it: projects, tasks, comments, files, memberships, billing records. One call, one transaction.

**User account deletion.** GDPR/privacy compliance — delete a user and cascade to their posts, comments, likes, sessions, API keys, notification preferences. Soft-delete what needs audit trails, hard-delete everything else.

**Content management.** Delete a blog post and its comments, tags (junction rows), revisions, and media attachments. Tags themselves survive — only the association is removed.

**E-commerce.** Delete a product category and unlink all products (SET NULL) so they become uncategorized instead of deleted. Or delete a store and cascade to all products, orders, and reviews.

**Testing and development.** Clean up test data with a single call instead of manually deleting in reverse FK order.

## Installation

```bash
go get github.com/khanakia/entx/entcascade
```

## Setup

Register the extension in your ent codegen entry point:

```go
//go:build ignore

package main

import (
    "log"

    "entgo.io/ent/entc"
    "entgo.io/ent/entc/gen"
    "github.com/khanakia/entx/entcascade"
)

func main() {
    config := &gen.Config{
        Target:  "./ent",
        Package: "your-project/ent",
    }
    opts := []entc.Option{
        entc.Extensions(
            entcascade.NewExtension(),
        ),
    }
    if err := entc.Generate("./schema", config, opts...); err != nil {
        log.Fatalf("running ent codegen: %v", err)
    }
}
```

Run `go run entc.go` to generate. A `cascade_delete.go` file is produced in your ent output directory.

## Usage

### Basic cascade

Annotate a schema to cascade-delete all its forward edges:

```go
func (User) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}
```

Generated:

```go
// Delete user and all dependents (posts, comments, profile, etc.) in a transaction.
err := ent.CascadeDeleteUser(ctx, client, userID)
```

### Skip edges

Exclude specific edges from the cascade:

```go
entcascade.Cascade(
    entcascade.SkipEdges("owner", "created_by"),
)
```

The `owner` and `created_by` edges won't be touched during cascade delete.

### Soft delete

If the target type has a `deleted_at` field, entcascade auto-detects it and generates `UPDATE SET deleted_at = now()` instead of `DELETE`.

```go
// Revision schema has a `deleted_at` field.
// Annotate the parent — no extra config needed:
func (Article) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}
```

Generated:

```go
// Soft-deletes revisions (sets deleted_at), then hard-deletes the article.
err := ent.CascadeDeleteArticle(ctx, client, articleID)
```

Override the auto-detection:

```go
entcascade.Cascade(
    entcascade.WithSoftDelete("files", "removed_at"),   // custom field name
    entcascade.WithHardDelete("temp_items"),             // force hard delete even if deleted_at exists
)
```

### Unlink (SET NULL)

Clear the foreign key instead of deleting the target. The child entity survives:

```go
func (Category) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(
            entcascade.WithUnlink("posts"),
        ),
    }
}
```

Generated:

```go
// Clears category_id on all posts, then deletes the category.
// Posts survive with category_id = NULL.
err := ent.CascadeDeleteCategory(ctx, client, categoryID)
```

### Batch delete

Delete multiple entities in a single transaction:

```go
err := ent.CascadeDeleteUserBatch(ctx, client, []int{1, 2, 3})
```

Empty slices are a no-op (no transaction started).

### Pre/Post hooks

Run business logic inside the cascade transaction:

```go
err := ent.CascadeDeleteUserWithHooks(ctx, client, userID, ent.CascadeDeleteUserHooks{
    Pre: func(ctx context.Context, c *ent.Client, id int) error {
        // Runs before any deletes. Return error to abort + rollback.
        log.Printf("about to delete user %d", id)
        return nil
    },
    Post: func(ctx context.Context, c *ent.Client, id int) error {
        // Runs after all deletes, before commit.
        return notifyUserDeleted(ctx, id)
    },
})
```

Both hooks receive the transaction client — reads and writes happen inside the same transaction.

## Annotation Reference

| Function | Description |
|----------|-------------|
| `Cascade()` | Enable cascade delete for all forward edges |
| `SkipEdges("a", "b")` | Exclude specific edges from cascade |
| `WithSoftDelete("edge", "field")` | Force soft delete using the specified field name |
| `WithHardDelete("edge")` | Force hard delete even if target has a soft-delete field |
| `WithUnlink("edge")` | Clear FK (SET NULL) instead of deleting the target |

Combine them:

```go
entcascade.Cascade(
    entcascade.SkipEdges("owner"),
    entcascade.WithSoftDelete("files", "deleted_at"),
    entcascade.WithHardDelete("temp_data"),
    entcascade.WithUnlink("channels"),
)
```

## Generated Functions

For each annotated type (e.g., `User`), six functions are generated:

| Function | Description |
|----------|-------------|
| `CascadeDeleteUser(ctx, client, id)` | Single delete in a transaction |
| `CascadeDeleteUserWithHooks(ctx, client, id, hooks)` | Single delete with pre/post hooks |
| `CascadeDeleteUserBatch(ctx, client, ids)` | Batch delete in a single transaction |
| `CascadeDeleteUserHooks` | Struct with `Pre`/`Post` callback fields |

## Edge Classification

The extension classifies edges and generates different delete strategies:

| Edge Type | Action |
|-----------|--------|
| O2M (one-to-many) | `DELETE WHERE fk = id` |
| O2O (FK on child) | `DELETE WHERE fk = id` |
| O2O (FK on owner, `OwnFK`) | Skipped |
| M2M with Through type | `DELETE FROM junction WHERE fk = id` |
| M2M without Through | Skipped (ent manages junction internally) |
| Inverse edges (`edge.From`) | Always skipped |

Nested cascades: if a child type is also annotated with `Cascade()`, its children are recursively deleted. Intermediate types' annotations are also respected — `SkipEdges`, `WithUnlink`, `WithSoftDelete`, and `WithHardDelete` rules on a child type apply when a parent cascade traverses through it. Cycle detection prevents infinite loops.

## Transaction Behavior

- All operations run inside a single transaction
- Any error triggers automatic rollback
- If called with `tx.Client()`, uses a savepoint
- Delete operations use `WHERE id = ?` (not `DeleteOneID`), so deleting an already-deleted entity returns 0 rows instead of an error — making cascades idempotent

## Example: Full Schema

```go
// schema/user.go
func (User) Edges() []ent.Edge {
    return []ent.Edge{
        edge.To("posts", Post.Type),
        edge.To("profile", Profile.Type).Unique(),
    }
}

func (User) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}

// schema/post.go
func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("author", User.Type).Ref("posts").Unique().Field("author_id"),
        edge.To("comments", Comment.Type),
        edge.To("tags", Tag.Type).Through("post_tags", PostTag.Type),
    }
}

func (Post) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entcascade.Cascade(),
    }
}
```

Deleting a user cascades: User → Posts → Comments + PostTags (junction). Tags survive.

```go
err := ent.CascadeDeleteUser(ctx, client, userID)
```
