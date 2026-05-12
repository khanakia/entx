// Package helper provides runtime helpers for entpoly polymorphic relations
// that do not lend themselves to codegen — Toggle / Sync semantics on top of
// the generated ent client builders.
//
// These helpers are intentionally small and reflection-free; they operate
// over slices of IDs, leaving query/attach mechanics to the typed builders
// emitted by entpoly's codegen.
//
// Notes:
//
//   - These functions are PURE. They never touch the database, never see
//     an ent client. Inputs and outputs are plain slices of comparable
//     IDs. This isolation is deliberate — it keeps the helper package
//     free of any ent-shape dependency, so users can vendor it without
//     pulling in the rest of entpoly's codegen surface.
//
//   - The set-diff functions treat duplicate IDs in the input as the
//     same logical element. Tests in helper_test.go document this
//     contract; do not change it without updating the corresponding
//     tests AND adding a CHANGELOG note (it's a behaviour change for
//     any caller relying on multiset semantics).
//
//   - When adding a new helper here, keep it allocation-light and
//     comparable-generic. Pattern: take two slices, return one or two
//     slices, never a map. If you need a map, return one separately —
//     do not embed inside another return value.
package helper

// Toggle returns (toAttach, toDetach) given the currently-attached set and
// the set the caller wants to flip. It is the building block of a
// Laravel-style toggle() over a polymorphic many-to-many.
//
// Usage with a generated client (illustrative):
//
//	attached, _ := client.Tag.Query().
//	    Where(tag.HasPostsWith(post.IDEQ(p.ID))).
//	    IDs(ctx)
//	toAdd, toDel := helper.Toggle(attached, []int{1, 2, 3})
//	// then call your typed Add / Remove builders with toAdd / toDel
func Toggle[ID comparable](attached, target []ID) (toAttach, toDetach []ID) {
	cur := make(map[ID]struct{}, len(attached))
	for _, id := range attached {
		cur[id] = struct{}{}
	}
	tgt := make(map[ID]struct{}, len(target))
	for _, id := range target {
		tgt[id] = struct{}{}
	}
	for id := range tgt {
		if _, ok := cur[id]; ok {
			toDetach = append(toDetach, id)
		} else {
			toAttach = append(toAttach, id)
		}
	}
	return
}

// Sync returns (toAttach, toDetach) such that applying both produces a
// set exactly equal to target. Mirrors Laravel's $rel->sync([ids]).
func Sync[ID comparable](attached, target []ID) (toAttach, toDetach []ID) {
	cur := make(map[ID]struct{}, len(attached))
	for _, id := range attached {
		cur[id] = struct{}{}
	}
	tgt := make(map[ID]struct{}, len(target))
	for _, id := range target {
		tgt[id] = struct{}{}
	}
	for id := range tgt {
		if _, ok := cur[id]; !ok {
			toAttach = append(toAttach, id)
		}
	}
	for id := range cur {
		if _, ok := tgt[id]; !ok {
			toDetach = append(toDetach, id)
		}
	}
	return
}

// SyncWithoutDetach returns the subset of target that is not yet attached.
// Equivalent to Laravel's $rel->syncWithoutDetaching([ids]).
func SyncWithoutDetach[ID comparable](attached, target []ID) []ID {
	cur := make(map[ID]struct{}, len(attached))
	for _, id := range attached {
		cur[id] = struct{}{}
	}
	var out []ID
	for _, id := range target {
		if _, ok := cur[id]; !ok {
			out = append(out, id)
			cur[id] = struct{}{}
		}
	}
	return out
}
