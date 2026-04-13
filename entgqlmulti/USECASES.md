# Why entgqlmulti?

**Real products rarely expose one GraphQL API to one audience.** You almost always end up with more than one:

- a **dashboard / admin** API for authenticated internal users
- a **public** API for the end-user-facing frontend
- a **mobile** API with a trimmed-down projection for bandwidth
- sometimes a **partner** API with contractual surface guarantees

But `entgql` generates exactly one monolithic `ent.graphql` for one API. The moment you need a second API, you're writing GraphQL by hand — typing the same `User`, `PostConnection`, `WhereInput`, `Order`, `CreateInput` types for the second API, the third, the fourth. Every schema change fans out into N schema files that drift from each other and from the ent source of truth.

`entgqlmulti` generates a separate `.graphql` file per API from a single set of annotations on your ent schemas. One source, many schemas, zero drift.

This document walks through every knob `entgqlmulti` exposes and what each would cost you to do by hand.

---

## The headline example

```go
// schema/chatbot.go
func (Chatbot) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entgql.RelayConnection(),
        entgql.QueryField(),
        entgql.Mutations(entgql.MutationCreate(), entgql.MutationUpdate()),

        entgqlmulti.ApiConfig(map[string][]entgqlmulti.ApiTarget{
            "apidash": {{Query: true, Mutations: true, Filters: true, OrderBy: true}},
            "apipub":  {{
                TypeName: "PublicChatbot",
                Fields:   []string{"name", "avatar", "description"},
                Query:    true,
            }},
        }),
    }
}
```

After `go generate ./...`:

```
api/apidash/schema.graphql   # full Chatbot, connection, where, order, mutations
api/apipub/schema.graphql    # PublicChatbot (3 fields), connection, query only
```

Add a new ent field? Regenerate. Both schemas update. No drift. No manual type-copy. No "oops we forgot to hide that field in the public API."

---

## Use cases, one by one

### 1. Full CRUD dashboard

**Problem.** Your internal dashboard needs every field, every mutation, filter and ordering. By hand, you write the full GraphQL surface for every entity. Then you keep it in sync with ent forever.

**entgqlmulti.** One target, four flags:

```go
"apidash": {{
    Query: true, Mutations: true, Filters: true, OrderBy: true,
}},
```

Generated output: `Chatbot` type with every public field, `ChatbotConnection` + `ChatbotEdge`, `ChatbotWhereInput`, `ChatbotOrder` + `ChatbotOrderField`, `CreateChatbotInput`, `UpdateChatbotInput`, `chatbots(...): ChatbotConnection!`, `createChatbot`, `updateChatbot`. All derived from the ent schema; regenerate whenever the schema changes.

---

### 2. Read-only public API with a subset of fields

**Problem.** The public frontend needs a chatbot card: name, avatar, description. It must not see `api_key`, internal `status`, or any mutation. Writing this by hand means defining a second `Chatbot` type, carefully enumerating the three safe fields, and making sure nobody ever adds a sensitive field to the public type by mistake.

**entgqlmulti.**

```go
"apipub": {{
    TypeName: "PublicChatbot",
    Fields:   []string{"name", "avatar", "description"},
    Query:    true,  // Mutations / Filters / OrderBy all default to false
}},
```

Generated output:

```graphql
type PublicChatbot @goModel(model: ".../ent.Chatbot") {
  id: ID!
  name: String!
  avatar: String
  description: String
}
```

The `@goModel` directive tells gqlgen to bind `PublicChatbot` to `*ent.Chatbot` — your resolver just returns the ent struct and gqlgen serializes only the three declared fields. Adding a new field to ent doesn't leak into the public schema unless you explicitly add it to `Fields`.

No mutation fields emitted → the whole `Mutation` root is omitted if no target in this API sets `Mutations: true`.

---

### 3. Two GraphQL types from the same ent entity

**Problem.** Your dashboard wants a full `Chatbot` type and also a lightweight `ChatbotSummary` for listing pages. These are the same rows in the DB, but you want different GraphQL shapes — and you don't want to invent a new ent entity just to get a second type.

**entgqlmulti.** Multiple `ApiTarget`s per API:

```go
"apidash": {
    {Query: true, Mutations: true, Filters: true, OrderBy: true},
    {
        TypeName:  "ChatbotSummary",
        Fields:    []string{"id", "name", "status"},
        Query:     true,
        QueryName: "chatbotSummaries",
    },
},
```

Generated output: `Chatbot` + `ChatbotSummary` side by side, two query fields (`chatbots`, `chatbotSummaries`), both resolving to the same underlying `*ent.Chatbot` via `@goModel`.

**Gotcha it handles for you.** Two GraphQL types mapped to one Go struct would cause gqlgen to emit a `node(id: ID!) : Node` resolver with duplicate cases and fail to compile. `entgqlmulti` unconditionally strips the `Node` interface from subset types — your Relay pagination still works, but the type switch stays unique.

---

### 4. Custom query field name (`QueryName`)

**Problem.** The auto-derived field name doesn't read well. `publicChatbots` isn't what you want on a mobile API — you want `me`.

**entgqlmulti.**

```go
"apimobile": {{
    TypeName:  "MobileUser",
    Fields:    []string{"id", "firstName", "lastName"},
    Query:     true,
    QueryName: "me",  // otherwise "mobileUsers"
}},
```

Any string works — `me`, `currentUser`, `partnerProfile`, whatever reads right in the API you're shipping.

---

### 5. Edges to entities not in this API

**Problem.** `User` has `posts: [Post!]` via `edge.To("posts", Post.Type)`. The public API exposes users but not posts (posts are internal). By hand you'd have to define the public User type from scratch, omitting the `posts` field. When you eventually add a `status` field to User, you'd have to remember to add it to the public type too.

**entgqlmulti.** You don't opt into Post in the public API's `ApiConfig`. `entgqlmulti` sees Post isn't a present type in apipub and automatically strips the `posts` field from `User` in that API's output. No manual maintenance. The rest of `User` still comes through.

```go
// Post.Annotations includes only "apidash" — no apipub target.
// User.Annotations includes both apidash and apipub.

// apidash/schema.graphql: User { ...; posts: [Post!] }   (Post exists)
// apipub/schema.graphql:  User { ... }                    (posts stripped)
```

The same applies to `hasPostsWith: [PostWhereInput!]` predicates inside `UserWhereInput`: pruned automatically (see use case 7). `hasPosts: Boolean` survives — it doesn't require `PostWhereInput` to exist.

---

### 6. Filtering with field restrictions

**Problem.** Your public API lets users filter the directory by name, but not by `apiKey` or by edge relations. The WhereInput generated by entgql has a predicate for every field and every edge. By hand you'd manually author a subset WhereInput.

**entgqlmulti.** When `Fields` is set and `Filters: true`, the WhereInput is filtered to the whitelisted fields automatically:

```go
"apipub": {{
    Fields:  []string{"id", "name"},
    Query:   true,
    Filters: true,
}},
```

Generated output keeps only `id`-based predicates, `name`-prefixed predicates (`name`, `nameNEQ`, `nameIn`, `nameContains`, `nameHasPrefix`, …), the logical operators (`not`, `and`, `or`), and any `has*`/`has*With` edge predicates that can actually be satisfied in this API (see use case 7).

---

### 7. Automatic `WhereInput` edge-predicate pruning

**Problem.** Bug-class that bites by-hand GraphQL splits regularly: `UserWhereInput` references `hasPostsWith: [PostWhereInput!]`. You split the schema, put `UserWhereInput` in your public schema, and gqlgen fails to compile because `PostWhereInput` isn't defined here. By hand you now have to audit every WhereInput for orphan edge references.

**entgqlmulti.** After filtering, it runs a post-pass (`pruneOrphanWhereInputFields`) that walks every `*WhereInput` in the per-API schema and drops any field whose resolved type is a `*WhereInput` that isn't present in this API. Scalar flags (`hasPosts: Boolean`) survive; relation predicates (`hasPostsWith: [...]`) are removed.

You get valid SDL with no orphan references, every time, across any mix of entity presence/absence.

---

### 8. snake_case + camelCase `Fields` interchangeably

**Problem.** ent's generated field constants are snake_case (`chatbot.FieldApiKey` = `"api_key"`). GraphQL fields are camelCase (`apiKey`). By hand you'd pick one convention and stick to it. But `Fields` entries in this package can legitimately come from either world — pasted from ent constants, or typed out from a GraphQL query the designer wrote.

**entgqlmulti.** Both forms accepted transparently. The normalizer pipes through `snake()` then `camel()`:

```go
// All three forms identify the same field:
Fields: []string{"first_name"}
Fields: []string{"firstName"}
Fields: []string{chatbot.FieldFirstName}   // "first_name"
```

Behind the scenes, camelCase input would otherwise be lowercased by ent's `camel()` function and silently drop the field. This is handled transparently so you don't need to think about it.

---

### 9. `OrderBy` without hand-authoring Order types

**Problem.** Order input types and `OrderField` enums are verbose — five fields in the type, one enum value per orderable field, plus the enum declaration. Five entities, five APIs, you've written 25 Order types.

**entgqlmulti.** Annotate orderable fields with `entgql.OrderField("FIELD_NAME")` once on the ent schema. Any API that opts into `OrderBy: true` gets the full Order input + OrderField enum copied in automatically:

```go
// ent schema
field.String("name").Annotations(entgql.OrderField("NAME")),

// api target
{Query: true, OrderBy: true}
```

Generated: `input ChatbotOrder { direction: OrderDirection! field: ChatbotOrderField! }` + `enum ChatbotOrderField { NAME CREATED_AT }` + `orderBy: [ChatbotOrder!]` argument on the query. All from one annotation per field.

---

### 10. Mutation inputs respect the field whitelist

**Problem.** A partner API lets you update a chatbot's name but not its `api_key`. By hand you'd author a `PartnerUpdateChatbotInput` that omits the forbidden fields. Then you'd write a gqlgen resolver that accepts this narrower input and forwards into the ent update. Every new ent field is a fresh decision: do partners get it? Remember to include/exclude.

**entgqlmulti.** `Mutations: true` + a `Fields` whitelist trims the generated `CreateXInput` and `UpdateXInput` to only the allowed fields:

```go
"apipartner": {{
    Fields:    []string{"id", "name"},
    Query:     true,
    Mutations: true,
}},
```

Generated: `CreateChatbotInput { name: String! }` and `UpdateChatbotInput { name: String }`. `apiKey` is not in the surface at all — partners can't even submit it, let alone overwrite it.

---

### 11. Entities that stay hidden from all per-API schemas

**Problem.** Some ent tables — audit logs, internal secrets, feature flag overrides — exist for server-side use only. They shouldn't show up in any GraphQL surface. By hand, "don't forget to exclude this one" becomes a review checklist.

**entgqlmulti.** Omit `entgqlmulti.ApiConfig()` from the schema entirely. Without the annotation, the entity is never included in any per-API output. Still fully usable on the server side via the ent client.

```go
// schema/secret.go — no ApiConfig → absent from every per-API schema
func (Secret) Annotations() []schema.Annotation {
    return []schema.Annotation{entgql.Skip(entgql.SkipAll)}
}
```

Any edge pointing to `Secret` from a different entity is also pruned from those other entities' per-API output (see use case 5).

---

## What you give up by doing it by hand

Everything above is tested end-to-end in `testentgqlmulti/` (see `testentgqlmulti/TESTS.md`). Each generated `.graphql` gets gqlgen-compiled, wired to a shared ent client, and exercised through real GraphQL-over-HTTP calls. Regenerating anything — schema change, annotation change, new API — reruns the whole suite.

The alternative is a hand-maintained GraphQL schema per API that drifts silently from ent. Every schema change is another N manual edits, another chance to leak a sensitive field, another chance to break gqlgen compilation with an orphan reference. The number you can get away with on willpower alone is usually one.

`entgqlmulti` is the "one annotation, many schemas" button.
