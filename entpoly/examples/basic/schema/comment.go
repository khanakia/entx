package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

// Comment is the canonical polymorphic child — its body belongs to any
// commentable parent (Post or Video). The relation is fully declared via
// the MorphMixin (which adds commentable_id + commentable_type columns)
// plus the MorphTo edge (which carries the metadata + allowed parent
// types).
type Comment struct{ ent.Schema }

// Mixin pulls in entpoly's discriminator columns. Without this line the
// preprocess hook errors out at codegen time with a "missing column"
// diagnostic.
func (Comment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		// MixinAllowed promotes the type column to a real enum so the
		// database (CHECK constraint / native ENUM type) and ent's
		// predicate package both enforce the closed set. Values must
		// agree with the AllowedTypes passed to the MorphTo edge below
		// — preprocess catches drift at codegen time.
		entpoly.MorphMixin("commentable",
			entpoly.MixinAllowed(Post.Type, Video.Type),
		),
	}
}

func (Comment) Fields() []ent.Field {
	return []ent.Field{
		field.Text("body"),
	}
}

func (Comment) Edges() []ent.Edge {
	return []ent.Edge{
		// MorphTo declares the polymorphic side: the discriminator pair
		// can point at either a Post or a Video. The parent types are
		// passed via the ent .Type method-value idiom.
		//
		// .Required() opts into the runtime-enforcement hook generated
		// by entpoly (see RegisterPolyHooks in polymorphic.go). A Save
		// that leaves both discriminator columns unset — or one that
		// clears them on update — is rejected with a typed error.
		// .Required() = compile / runtime guarantee the relation is set.
		// .Touch()    = bump the parent's updated_at on every Comment Save
		//               (Laravel $touches). Both opt-ins require
		//               RegisterPolyHooks(client) at startup.
		entpoly.MorphTo("commentable", Post.Type, Video.Type).
			Required().
			Touch(),
	}
}
