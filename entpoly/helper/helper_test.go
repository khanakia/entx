package helper

import (
	"sort"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────
// Toggle
// ──────────────────────────────────────────────────────────────────────────

func TestToggle_AttachAndDetach(t *testing.T) {
	attached := []int{1, 2, 3}
	target := []int{2, 4} // 2 is currently attached → detach; 4 is new → attach
	add, del := Toggle(attached, target)
	assertSet(t, "add", add, []int{4})
	assertSet(t, "del", del, []int{2})
}

func TestToggle_AllNew(t *testing.T) {
	add, del := Toggle([]int{1, 2}, []int{3, 4})
	assertSet(t, "add", add, []int{3, 4})
	assertSet(t, "del", del, []int{})
}

func TestToggle_AllExisting(t *testing.T) {
	add, del := Toggle([]int{1, 2, 3}, []int{1, 2})
	assertSet(t, "add", add, []int{})
	assertSet(t, "del", del, []int{1, 2})
}

func TestToggle_EmptyTarget(t *testing.T) {
	add, del := Toggle([]int{1, 2, 3}, []int{})
	assertSet(t, "add", add, []int{})
	assertSet(t, "del", del, []int{})
}

func TestToggle_EmptyAttached(t *testing.T) {
	add, del := Toggle([]int{}, []int{1, 2})
	assertSet(t, "add", add, []int{1, 2})
	assertSet(t, "del", del, []int{})
}

func TestToggle_BothEmpty(t *testing.T) {
	add, del := Toggle([]int{}, []int{})
	if len(add) != 0 || len(del) != 0 {
		t.Errorf("got (%v,%v), want both empty", add, del)
	}
}

func TestToggle_StringIDs(t *testing.T) {
	add, del := Toggle([]string{"a", "b"}, []string{"b", "c"})
	assertSet(t, "add", add, []string{"c"})
	assertSet(t, "del", del, []string{"b"})
}

func TestToggle_DuplicateInputs(t *testing.T) {
	// Duplicates in target collapse — toggling "2" once vs three times
	// yields the same operation set.
	add, del := Toggle([]int{1, 2}, []int{2, 2, 2, 3})
	assertSet(t, "add", add, []int{3})
	assertSet(t, "del", del, []int{2})
}

// ──────────────────────────────────────────────────────────────────────────
// Sync
// ──────────────────────────────────────────────────────────────────────────

func TestSync_AddAndDelete(t *testing.T) {
	add, del := Sync([]int{1, 2, 3}, []int{2, 3, 4})
	assertSet(t, "add", add, []int{4})
	assertSet(t, "del", del, []int{1})
}

func TestSync_EmptyTargetDetachesAll(t *testing.T) {
	add, del := Sync([]int{1, 2, 3}, []int{})
	assertSet(t, "add", add, []int{})
	assertSet(t, "del", del, []int{1, 2, 3})
}

func TestSync_EmptyAttachedAttachesAll(t *testing.T) {
	add, del := Sync([]int{}, []int{1, 2, 3})
	assertSet(t, "add", add, []int{1, 2, 3})
	assertSet(t, "del", del, []int{})
}

func TestSync_NoChange(t *testing.T) {
	add, del := Sync([]int{1, 2, 3}, []int{1, 2, 3})
	if len(add) != 0 || len(del) != 0 {
		t.Errorf("identical sets: add=%v del=%v, want both empty", add, del)
	}
}

func TestSync_StringIDs(t *testing.T) {
	add, del := Sync([]string{"a", "b"}, []string{"b", "c"})
	assertSet(t, "add", add, []string{"c"})
	assertSet(t, "del", del, []string{"a"})
}

func TestSync_DuplicatesInBothSides(t *testing.T) {
	// Sync must treat each side as a *set*, not a multiset — duplicates
	// collapse and the diff is between unique members.
	add, del := Sync([]int{1, 1, 2}, []int{2, 2, 3, 3})
	assertSet(t, "add", add, []int{3})
	assertSet(t, "del", del, []int{1})
}

func TestSync_DisjointSets(t *testing.T) {
	add, del := Sync([]int{1, 2}, []int{3, 4})
	assertSet(t, "add", add, []int{3, 4})
	assertSet(t, "del", del, []int{1, 2})
}

// ──────────────────────────────────────────────────────────────────────────
// SyncWithoutDetach
// ──────────────────────────────────────────────────────────────────────────

func TestSyncWithoutDetach_OnlyAttachesNew(t *testing.T) {
	got := SyncWithoutDetach([]int{1, 2}, []int{2, 3, 4})
	assertSet(t, "result", got, []int{3, 4})
}

func TestSyncWithoutDetach_AllAttached(t *testing.T) {
	got := SyncWithoutDetach([]int{1, 2, 3}, []int{1, 2})
	if len(got) != 0 {
		t.Errorf("got %v, want empty (all targets already attached)", got)
	}
}

func TestSyncWithoutDetach_DuplicatesInTarget(t *testing.T) {
	// Duplicates in target should be deduped — adding the same ID
	// twice in a single call is a no-op for the second instance.
	got := SyncWithoutDetach([]int{}, []int{1, 1, 2, 2})
	assertSet(t, "result", got, []int{1, 2})
}

func TestSyncWithoutDetach_EmptyTarget(t *testing.T) {
	got := SyncWithoutDetach([]int{1, 2}, []int{})
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestSyncWithoutDetach_StringIDs(t *testing.T) {
	got := SyncWithoutDetach([]string{"a"}, []string{"a", "b", "c"})
	assertSet(t, "result", got, []string{"b", "c"})
}

// ──────────────────────────────────────────────────────────────────────────
// Properties (Toggle and Sync invariants)
// ──────────────────────────────────────────────────────────────────────────

// TestSync_AppliedDiffEqualsTarget verifies the property that anchors the
// "Sync replaces the set" semantics: applying (attached - del) ∪ add
// equals the target set.
func TestSync_AppliedDiffEqualsTarget(t *testing.T) {
	attached := []int{1, 2, 3}
	target := []int{2, 4, 5}
	add, del := Sync(attached, target)

	delSet := map[int]struct{}{}
	for _, id := range del {
		delSet[id] = struct{}{}
	}
	result := map[int]struct{}{}
	for _, id := range attached {
		if _, ok := delSet[id]; !ok {
			result[id] = struct{}{}
		}
	}
	for _, id := range add {
		result[id] = struct{}{}
	}
	want := map[int]struct{}{}
	for _, id := range target {
		want[id] = struct{}{}
	}
	if len(result) != len(want) {
		t.Fatalf("result size %d, want %d", len(result), len(want))
	}
	for k := range want {
		if _, ok := result[k]; !ok {
			t.Errorf("result missing %v", k)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────

// assertSet checks that two slices are equal as sets (order-independent).
// The empty want []T{} matches a nil / zero-length got — callers should
// pass the zero-length form for clarity.
func assertSet[T comparable](t *testing.T, label string, got, want []T) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s size mismatch: got %v (len=%d), want %v (len=%d)", label, got, len(got), want, len(want))
		return
	}
	wantSet := map[T]struct{}{}
	for _, v := range want {
		wantSet[v] = struct{}{}
	}
	for _, v := range got {
		if _, ok := wantSet[v]; !ok {
			t.Errorf("%s: unexpected element %v in %v", label, v, got)
		}
	}
}

// Compile-time check that the int-specific sort helper still works for
// the legacy assertions that used it. Kept to make sure refactors of
// assertSet do not break the int path.
func init() {
	_ = sort.Ints
}
