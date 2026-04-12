# entgqlmulti

An [ent](https://entgo.io) + [entgql](https://entgo.io/docs/graphql/) extension that generates separate GraphQL schemas for different APIs from a single ent schema.

## Problem

When you have multiple APIs (dashboard, public, mobile) backed by the same ent schema, `entgql` generates a single monolithic GraphQL schema. You end up manually splitting types, hiding fields, and maintaining separate schema files — all of which drift as the schema evolves.

## Solution

Annotate your ent schemas with `entgqlmulti.ApiConfig()` to declare which types, fields, and operations each API gets. The generator produces separate `.graphql` files per API, all derived from the same source of truth.

## Installation

```bash
go get github.com/khanakia/entx/entgqlmulti
```

## Setup

Wire the generator into your entgql schema hook:

```go
//go:build ignore

package main

import (
    "log"

    "entgo.io/contrib/entgql"
    "entgo.io/ent/entc"
    "entgo.io/ent/entc/gen"
    "github.com/khanakia/entx/entgqlmulti"
)

func main() {
    gqlExt, err := entgql.NewExtension(
        entgql.WithWhereInputs(true),
        entgql.WithSchemaGenerator(),
        entgql.WithSchemaHook(
            entgqlmulti.New(
                entgqlmulti.WithEntPackage("your-project/ent"),
                entgqlmulti.WithDefaultAPI("apidash"),
                entgqlmulti.WithAPIOutputPath("apidash", "./api/dashboard/schema.graphql"),
                entgqlmulti.WithAPIOutputPath("apipub", "./api/public/schema.graphql"),
            ).SchemaHook(),
        ),
    )
    if err != nil {
        log.Fatalf("creating entgql extension: %v", err)
    }

    if err := entc.Generate("./schema", &gen.Config{}, entc.Extensions(gqlExt)); err != nil {
        log.Fatalf("running ent codegen: %v", err)
    }
}
```

## Usage

### Annotate schemas

Declare how each entity participates in each API:

```go
func (Chatbot) Annotations() []schema.Annotation {
    return []schema.Annotation{
        entgqlmulti.ApiConfig(map[string][]entgqlmulti.ApiTarget{
            "apidash": {
                {
                    Query:     true,
                    Mutations: true,
                    Filters:   true,
                    OrderBy:   true,
                },
            },
            "apipub": {
                {
                    TypeName: "PublicBot",
                    Fields:   []string{"name", "avatar", "description"},
                    Query:    true,
                },
            },
        }),
    }
}
```

### ApiTarget fields

| Field | Type | Description |
|-------|------|-------------|
| `TypeName` | `string` | Custom GraphQL type name (default: ent type name) |
| `Fields` | `[]string` | Subset of fields to include (default: all) |
| `Query` | `bool` | Generate root Query connection field |
| `QueryName` | `string` | Override the query field name |
| `Mutations` | `bool` | Generate Create/Update mutations |
| `Filters` | `bool` | Add WhereInput argument to query |
| `OrderBy` | `bool` | Add OrderBy argument to query |

### Generator options

| Option | Description |
|--------|-------------|
| `WithEntPackage(pkg)` | Go import path for ent types (used in `@goModel` directives) |
| `WithDefaultAPI(name)` | Default API name (default: `"apidash"`) |
| `WithAPIOutputPath(api, path)` | Output file path for each API's schema |

## Examples

### Read-only public API

Expose a subset of fields with no mutations:

```go
"apipub": {
    {
        TypeName: "PublicBot",
        Fields:   []string{"name", "avatar"},
        Query:    true,
    },
},
```

### Full CRUD dashboard API

```go
"apidash": {
    {
        Query:     true,
        Mutations: true,
        Filters:   true,
        OrderBy:   true,
    },
},
```

### Multiple types from one entity

Generate two GraphQL types from the same ent entity in one API:

```go
"apidash": {
    {
        TypeName: "Chatbot",
        Query:    true,
        Mutations: true,
    },
    {
        TypeName:  "ChatbotSummary",
        Fields:    []string{"id", "name", "status"},
        Query:     true,
        QueryName: "chatbotSummaries",
    },
},
```

### Field names

Both ent field names (snake_case) and GraphQL field names (camelCase) are accepted:

```go
Fields: []string{"first_name", "lastName", "email"}  // both work
```

## How It Works

1. `entgql` generates the full monolithic schema (all types, all fields)
2. `entgqlmulti` hooks into the schema generation pipeline
3. For each API, it filters the full schema down to only the annotated types and fields
4. Connection types (`XConnection`, `XEdge`), input types (`CreateXInput`, `WhereXInput`), and enums are generated per-API as needed
5. Each API gets its own `.graphql` file with `@goModel` directives pointing to the shared ent types
