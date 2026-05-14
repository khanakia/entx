# morphstring — coverage example: MorphMixin without MixinAllowed

`examples/basic` enables `entpoly.MixinAllowed(...)` on every `MorphMixin`,
so the polymorphic type column is emitted as `field.Enum`. ent therefore
generates a named string type (e.g. `comment.CommentableType`) that the
codegen template uses as a type conversion.

`examples/morphstring` exercises the *other* mode: `MorphMixin` without
`MixinAllowed`, so the type column is a plain `field.String`. In that
mode `comment.CommentableType` resolves to ent's predicate-EQ shortcut
function (not a type) — wrapping a value with it would be a function
call, not a cast. The template branches on `TypeIsEnum` and omits the
wrap in this mode.

Existing as a separate example so `go build ./...` and `go test ./...`
exercise both code paths. The `runtime_test.go` smoke test asserts that
the polymorphic setters, predicates, parent resolver, and `.GQL()`
union surface all work against a plain-string discriminator.

Regenerate after schema changes:

```
go run entc.go
```
