# Relationships reference

`entpoly` ports the full set of Laravel polymorphic relationship shapes onto ent as **schema-level edges**. Declarations live where they belong: a child's `MorphTo` in its `Edges()`, a parent's `MorphMany` in its `Edges()`, a holder's `MorphedByMany` in its `Edges()`. No annotations, no field-spreads, no `interface{}` casts on the read path.

Read [getting-started](./getting-started.md) first if you have not used `entpoly` before.

## Mental model

Two halves to every polymorphic relationship:

1. **The child** owns the discriminator pair: `<relation>_id` + `<relation>_type`. Examples: a `Comment` (the thing being commented on), an `Image` (the thing being illustrated), a `Taggable` pivot row (the thing being tagged).
2. **The parent** is whichever entity the child currently references. Multiple Go types can play this role simultaneously — that is the polymorphism.

The child carries two pieces of state per relation: an opaque id (string by default — accommodates any parent PK shape) and a stable morph key (the parent type's alias). `entpoly` generates a `Morphable` interface plus per-parent `MorphID()` / `MorphKey()` methods so you set the relation with a single typed call:

```go
comment.Update().SetCommentable(post).Save(ctx)
```

## Shape 1 — one-to-one polymorphic (`MorphOne` / `MorphTo`)

> A `Post` has one featured `Image`. A `User` has one profile `Image`. The same `Image` table backs both.

### Child schema (Image)

```go
type Image struct{ ent.Schema }

func (Image) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("imageable"),
    }
}

func (Image) Fields() []ent.Field {
    return []ent.Field{
        field.String("url"),
    }
}

func (Image) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("imageable", Post.Type, User.Type),
    }
}
```

### Parent schema (Post)

```go
func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphOne("featured_image", Image.Type, "imageable"),
    }
}
```

### Usage

```go
img, _ := client.Image.Create().SetURL("https://...").SetImageable(post).Save(ctx)
_ = img // img.ImageableID / img.ImageableType are now populated.

// Laravel: $post->image → entpoly: post.QueryFeaturedImage(ctx)
featured, err := post.QueryFeaturedImage(ctx)
// (nil, nil) when the post has no featured image; typed (*Image, nil) otherwise.

// And the reverse — img.QueryImageable(ctx) returns the sealed
// interface ImageImageableParent (only *Post here, since Image's
// AllowedTypes is just Post).
parent, _ := img.QueryImageable(ctx)
if p, ok := parent.(*ent.Post); ok {
    fmt.Println(p.Title)
}
```

For DB-enforced 1:1, add a unique index on the child:

```go
func (Image) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("imageable_type", "imageable_id").Unique(),
    }
}
```

## Shape 2 — one-to-many polymorphic (`MorphMany` / `MorphTo`)

> A `Post` has many `Comment`s. A `Video` has many `Comment`s. Same `Comment` table.

### Child schema (Comment)

```go
type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable"),
    }
}

func (Comment) Fields() []ent.Field {
    return []ent.Field{field.Text("body")}
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type),
    }
}
```

### Parent schemas (Post / Video)

```go
func (Post) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
    }
}

func (Video) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphMany("comments", Comment.Type, "commentable"),
    }
}
```

### Usage

```go
// Attach a comment to a post.
c, _ := client.Comment.Create().
    SetBody("Nice!").
    SetCommentable(post).   // sealed-interface param — Article fails to compile
    Save(ctx)

// Laravel: $post->comments  → entpoly: post.QueryComments()
comments, _ := post.QueryComments().
    Order(ent.Desc(comment.FieldCreatedAt)).
    All(ctx)

// Laravel: $comment->commentable → entpoly: c.QueryCommentable(ctx)
parent, _ := c.QueryCommentable(ctx)
switch p := parent.(type) {
case *ent.Post:  /* typed *Post  */
case *ent.Video: /* typed *Video */
case nil:        /* unset        */
}
// case *ent.Article: → compile error (not in AllowedTypes)
```

## Shape 3 — many-to-many polymorphic (`MorphedByMany` + polymorphic pivot)

> A `Tag` can be attached to many `Post`s **or** many `Video`s. The same `Tag` table is reused.

The pivot is the child of the polymorphic relation. The holder declares back-refs through that pivot.

### Pivot schema (Taggable)

```go
type Taggable struct{ ent.Schema }

func (Taggable) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("taggable"),
    }
}

func (Taggable) Fields() []ent.Field {
    return []ent.Field{
        field.Int("tag_id"),
        field.String("added_by").Optional(),
        field.Int("sort_order").Default(0),
    }
}

func (Taggable) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("taggable", Post.Type, Video.Type),
    }
}
```

Pivot extras (Laravel's `withPivot('added_by')`) are just regular ent fields on the pivot. Index, validate, and hook them exactly as any other entity.

### Holder schema (Tag)

```go
type Tag struct{ ent.Schema }

func (Tag) Fields() []ent.Field {
    return []ent.Field{field.String("name").Unique()}
}

func (Tag) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphedByMany("posts",  Post.Type).
            Through("taggables", Taggable.Type),
        entpoly.MorphedByMany("videos", Video.Type).
            Through("taggables", Taggable.Type),
    }
}
```

`Through(pivotTable, pivotType)` auto-derives the morph relation name from the singular form of the table name (`taggables` → `taggable`). Override via `.MorphName("...")` when the singular form is irregular.

### Usage

```go
// Tag a post with two tags.
golang, _ := client.Tag.Create().SetName("golang").Save(ctx)
db,     _ := client.Tag.Create().SetName("db").Save(ctx)

for _, tag := range []*ent.Tag{golang, db} {
    _, _ = client.Taggable.Create().
        SetTagID(tag.ID).
        SetTaggable(post).
        SetAddedBy("aman").
        Save(ctx)
}

// Tags attached to a post:
pivots, _ := client.Taggable.Query().
    Where(
        taggable.TaggableIDEQ(post.MorphID()),
        taggable.TaggableTypeEQ("post"),
    ).
    All(ctx)
```

Use the `helper.Toggle` / `helper.Sync` / `helper.SyncWithoutDetach` set-diff helpers when you need Laravel's `sync()` / `toggle()` semantics.

## Shape 4 — self-referential polymorphic

> A `Reaction` references *any* commentable target, including other reactions.

Self-reference is just shape 1 / shape 2 with the schema's own type listed in the parent set:

```go
type Reaction struct{ ent.Schema }

func (Reaction) Mixin() []ent.Mixin {
    return []ent.Mixin{entpoly.MorphMixin("reactable")}
}

func (Reaction) Fields() []ent.Field {
    return []ent.Field{field.String("emoji")}
}

func (Reaction) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("reactable", Post.Type, Video.Type, Reaction.Type),
    }
}
```

`entpoly` does not need special handling — the morph map registers `"reaction" → Reaction`, and `SetReactable(parentReaction)` works exactly like any other parent.

## Custom column names

Default columns are `<relation>_id` and `<relation>_type`. Override them via matching settings on the mixin **and** edge:

```go
func (Comment) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("commentable",
            entpoly.MixinIDColumn("parent_id"),
            entpoly.MixinTypeColumn("parent_type"),
            entpoly.MixinIDType("int"),
        ),
    }
}

func (Comment) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("commentable", Post.Type, Video.Type).
            IDColumn("parent_id").
            TypeColumn("parent_type").
            IDType("int"),
    }
}
```

Mismatched overrides surface at codegen time with a precise error including the right `MixinIDColumn(...)` / `MixinTypeColumn(...)` call to add.

## Choosing the id type

By default the morph id column is a `string`. This works for every PK shape — UUIDs, integers, ULIDs — because the parent's id is stringified on write via `fmt.Sprint(parent.ID)`.

Switch to `int64` only when every allowed parent uses an int PK:

```go
entpoly.MorphMixin("commentable", entpoly.MixinIDType("int"))
entpoly.MorphTo("commentable", Post.Type).IDType("int")
```

Both settings must match. A mismatch produces a Go compile error in the generated `polymorphic.go`.

## Foreign keys

Polymorphic columns intentionally carry **no** foreign-key constraint. The `*_id` column references multiple tables; SQL has no primitive for that. Consequences:

1. Orphan rows are possible if a parent row is deleted out from under a child. Either delete children explicitly (in a transaction) or pair with `entcascade` for application-level cascade deletes.
2. `entpoly` emits no `ON DELETE` clause. Strict per-type referential integrity requires a discriminated union (separate FK column per parent) instead.

A composite index on `(<name>_type, <name>_id)` is recommended for read performance. Declare it manually until v2 emits it automatically:

```go
func (Comment) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("commentable_type", "commentable_id"),
    }
}
```

## Multiple polymorphic relations on one schema

A single schema can declare any number of independent polymorphic relations. Each needs its own mixin and its own edge:

```go
type Audit struct{ ent.Schema }

func (Audit) Mixin() []ent.Mixin {
    return []ent.Mixin{
        entpoly.MorphMixin("actor"),
        entpoly.MorphMixin("target"),
    }
}

func (Audit) Fields() []ent.Field {
    return []ent.Field{field.String("action"), field.Time("at")}
}

func (Audit) Edges() []ent.Edge {
    return []ent.Edge{
        entpoly.MorphTo("actor",  User.Type, ServiceAccount.Type),
        entpoly.MorphTo("target", Post.Type, Video.Type, Comment.Type),
    }
}
```

The generated code emits separate `SetActor` / `ClearActor` and `SetTarget` / `ClearTarget` builders.

## Validation rules

`entpoly` surfaces the following errors at codegen time so the failure mode is visible at `go generate` rather than at runtime:

- `MorphTo` with no parent types — at least one `X.Type` argument is required.
- `MorphTo` declared without the matching `MorphMixin` — the discriminator columns are missing.
- `MorphTo` with column-name overrides that disagree with the mixin's overrides — the error message tells you exactly which mixin option to add.
- `MorphedByMany` without `.Through(...)` — the pivot is required.
- `MorphedByMany` with no parent type — the second argument is required.

If a polymorphic edge ever produces a confusing compile error inside `ent/polymorphic.go`, re-read the codegen log: the actual problem usually surfaces with a clear `entpoly:` prefix and a remediation hint.
