package testentpoly

import (
	"context"
	"strings"
	"sync"

	"entgo.io/ent/dialect"
)

// queryTracer wraps a dialect.Driver and records every Query call's SQL
// string. Exec calls are forwarded but not recorded — eager-load
// assertions only care about SELECTs, and ent issues SELECTs via Query.
//
// The recorded slice is mutex-guarded so concurrent ent operations (rare
// in these tests but possible) don't race.
type queryTracer struct {
	inner dialect.Driver

	mu      sync.Mutex
	queries []string
}

func (t *queryTracer) Exec(ctx context.Context, query string, args, v any) error {
	return t.inner.Exec(ctx, query, args, v)
}

func (t *queryTracer) Query(ctx context.Context, query string, args, v any) error {
	t.mu.Lock()
	t.queries = append(t.queries, query)
	t.mu.Unlock()
	return t.inner.Query(ctx, query, args, v)
}

func (t *queryTracer) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := t.inner.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return &tracedTx{inner: tx, parent: t}, nil
}

func (t *queryTracer) Close() error    { return t.inner.Close() }
func (t *queryTracer) Dialect() string { return t.inner.Dialect() }

// Reset clears the recorded query log. Call this between observation
// windows in tests (e.g. after migration, before the operation being
// measured).
func (t *queryTracer) Reset() {
	t.mu.Lock()
	t.queries = nil
	t.mu.Unlock()
}

// Snapshot returns a copy of the recorded queries.
func (t *queryTracer) Snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.queries))
	copy(out, t.queries)
	return out
}

// CountSelectsFrom returns how many recorded queries are SELECTs whose
// FROM clause mentions the given table (case-insensitive, matches
// "FROM `table`" or "FROM \"table\"" or unquoted FROM table). Used by
// eager-load tests to assert one SELECT per parent type.
func (t *queryTracer) CountSelectsFrom(table string) int {
	tbl := strings.ToLower(table)
	n := 0
	for _, q := range t.Snapshot() {
		lq := strings.ToLower(q)
		if !strings.HasPrefix(strings.TrimSpace(lq), "select") {
			continue
		}
		// Try common quotings.
		needles := []string{
			"from `" + tbl + "`",
			"from \"" + tbl + "\"",
			"from " + tbl + " ",
			"from " + tbl + "\n",
		}
		matched := false
		for _, n2 := range needles {
			if strings.Contains(lq, n2) {
				matched = true
				break
			}
		}
		if matched {
			n++
		}
	}
	return n
}

// tracedTx wraps a dialect.Tx so SELECTs issued inside transactions are
// also recorded.
type tracedTx struct {
	inner  dialect.Tx
	parent *queryTracer
}

func (t *tracedTx) Exec(ctx context.Context, query string, args, v any) error {
	return t.inner.Exec(ctx, query, args, v)
}

func (t *tracedTx) Query(ctx context.Context, query string, args, v any) error {
	t.parent.mu.Lock()
	t.parent.queries = append(t.parent.queries, query)
	t.parent.mu.Unlock()
	return t.inner.Query(ctx, query, args, v)
}

func (t *tracedTx) Commit() error   { return t.inner.Commit() }
func (t *tracedTx) Rollback() error { return t.inner.Rollback() }
