# testentpoly — GraphQL queries

Hand-crafted queries for exploring the polymorphic union surface emitted by [`entpoly`](../entpoly). Each one runs against the seeded server (`task serve`) — paste into the playground at `http://localhost:8080/` or fire from `curl`.

## Server

```bash
task serve                       # default :8080
PORT=9090 task serve             # override port
```

Seeded on startup: **2 Posts, 1 Video, 1 Image, 5 Comments** spread across the three parent types. Endpoints:

| Path | Use |
|---|---|
| `GET  /` | gqlgen playground (interactive UI) |
| `POST /query` | GraphQL endpoint |

## Schema surface (cheat-sheet)

```graphql
type Comment { id: ID! body: String! commentable: Commentable }
union Commentable = Post | Video | Image

type Query {
  comment(id: ID!): Comment
  comments: [Comment!]!
  post(id: ID!): Post
  video(id: ID!): Video
  document(id: UUID!): Document
}
```

`Commentable` is the polymorphic union — entpoly emits the SDL fragment + Go-side `Is<Union>()` markers so gqlgen recognises every member at runtime.

---

## 1. Forward union — every comment + its parent

```graphql
{
  comments {
    id
    body
    commentable {
      __typename
      ... on Post  { id title published }
      ... on Video { id title }
      ... on Image { id url }
    }
  }
}
```

```bash
curl -s -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ comments { id body commentable { __typename ... on Post { id title published } ... on Video { id title } ... on Image { id url } } } }"}' \
  | jq
```

Proves: same `commentable` field resolves to three different concrete Go types in one result set.

## 2. Single comment by id

```graphql
{
  comment(id: "4") {
    body
    commentable {
      __typename
      ... on Video { title }
    }
  }
}
```

```bash
curl -s -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ comment(id: \"4\") { body commentable { __typename ... on Video { title } } } }"}' \
  | jq
```

## 3. Parent by id (sanity — non-polymorphic path)

```graphql
{
  post(id: "1")  { id title published }
  video(id: "1") { id title }
}
```

```bash
curl -s -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ post(id: \"1\") { id title published } video(id: \"1\") { id title } }"}' \
  | jq
```

## 4. Type-narrow probe — only Post-backed comments materialize

```graphql
{
  comments {
    body
    commentable {
      ... on Post { title }
    }
  }
}
```

Video / Image comments still appear in the array; their `commentable` field comes back as `{}` because no fragment matched. Useful for type-filtered UIs.

## 5. `__typename` only — cheap discriminator

```graphql
{
  comments {
    id
    commentable { __typename }
  }
}
```

Proves the union resolver runs even without member fragments — useful when the client only needs to group by parent type before deciding what to render.

## 6. Aliases — multiple parent fetches in one round-trip

```graphql
{
  p1: post(id: "1")  { title }
  p2: post(id: "2")  { title published }
  v1: video(id: "1") { title }
}
```

## 7. Introspect the union

```graphql
{
  __type(name: "Commentable") {
    name
    kind
    possibleTypes { name }
  }
}
```

Expect:

```json
{ "name": "Commentable", "kind": "UNION", "possibleTypes": [
  { "name": "Post" }, { "name": "Video" }, { "name": "Image" }
]}
```

This confirms the `.GQL()` builder on the schema-side MorphTo actually shipped the union members through to the live SDL.

## 8. Negative — wrong fragment type rejected at validation

```graphql
{
  comments {
    commentable {
      ... on Comment { body }
    }
  }
}
```

Returns `GRAPHQL_VALIDATION_FAILED`:

```json
{"errors":[{"message":"Fragment cannot be spread here as objects of type \"Commentable\" can never be of type \"Comment\".", ... }]}
```

Proves union membership is enforced at the GraphQL layer (not just the Go layer).

## 9. Full curl-only smoke pass

A one-shot you can run against any deploy to verify the union surface is alive:

```bash
curl -fsS -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ comments { commentable { __typename } } }"}' \
  | jq -e '.data.comments | length > 0' >/dev/null \
  && echo "✓ polymorphic union live" \
  || echo "✗ union surface broken"
```

## Variables — using the `variables` form

When the client passes the id rather than inlining it:

```graphql
query GetComment($id: ID!) {
  comment(id: $id) {
    body
    commentable { __typename }
  }
}
```

```bash
curl -s -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"query GetComment($id: ID!) { comment(id: $id) { body commentable { __typename } } }", "variables": {"id":"1"}}' \
  | jq
```

---

## See also

- [`SCENARIOS.md`](./SCENARIOS.md) — full coverage matrix (every scenario covered by an automated test).
- [`gql_test.go`](./gql_test.go) — the same queries above, run as part of `task test`.
- [`../entpoly/docs/laravel-parity.md`](../entpoly/docs/laravel-parity.md#what-laravel-has-that-we-dont-yet-v2-backlog) — how `.GQL()` maps to Laravel's GraphQL conventions.
