package runtime

import (
	"encoding/json"
	"strings"

	"github.com/atotto/clipboard"
)

// Clipboard shortcuts:
//
//   y  → copy the focused cell value (table) / current row's title (browser)
//   Y  → copy the whole row as TAB-separated values (all visible columns)
//
// Both views go through copyToClipboard so the OS clipboard target +
// status-message style stay consistent. On headless systems where
// atotto/clipboard can't reach a clipboard (no xclip, no DISPLAY) we
// surface the error inline; nothing crashes.

func copyToClipboard(h *modalHost, text, label string) {
	if text == "" {
		h.updateStatus("nothing to copy")
		return
	}
	if err := clipboard.WriteAll(text); err != nil {
		h.updateStatus("clipboard error: " + err.Error())
		return
	}
	preview := text
	if len(preview) > 40 {
		preview = preview[:40] + "…"
	}
	h.updateStatus("copied " + label + ": " + preview)
}

// --- tableView wrappers ---

func (t *tableView) copyFocusedCell() {
	row, col := t.table.GetSelection()
	if row < 1 || row-1 >= len(t.rows) {
		return
	}
	cols := t.visibleColumns()
	col -= t.colOffset() // skip the dedicated # row-number column
	if col < 0 || col >= len(cols) {
		return
	}
	v := t.rows[row-1].Columns[cols[col].key]
	copyToClipboard(t.host(), v, cols[col].label)
}

func (t *tableView) copyFocusedRow() {
	row, _ := t.table.GetSelection()
	if row < 1 || row-1 >= len(t.rows) {
		return
	}
	cols := t.visibleColumns()
	r := t.rows[row-1]
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		parts = append(parts, r.Columns[c.key])
	}
	copyToClipboard(t.host(), strings.Join(parts, "\t"), "row")
}

// --- browser wrappers ---

func (b *browser) copyFocusedID() {
	idx := b.list.GetCurrentItem()
	if idx < 0 || idx >= len(b.rows) {
		return
	}
	copyToClipboard(b.host(), b.rows[idx].ID, "id")
}

func (b *browser) copyFocusedRow() {
	idx := b.list.GetCurrentItem()
	if idx < 0 || idx >= len(b.rows) {
		return
	}
	r := b.rows[idx]
	// Generic TSV: id first, then every visible column the spec
	// declares in declaration order. No hardcoded field names — works
	// against any schema shape.
	parts := []string{r.ID}
	for _, c := range b.spec.columns {
		if c.hidden || c.key == "id" {
			continue
		}
		parts = append(parts, r.Columns[c.key])
	}
	copyToClipboard(b.host(), strings.Join(parts, "\t"), "row")
}

// rowAsJSON renders one Row as a pretty-printed JSON object.
//
// Prefers the row's ent-native JSON (Row.JSON, set by codegen via
// `json.Marshal(r)` on the typed *ent.X) so the output matches ent's
// native struct shape — `id`, every column field, and an `edges` map
// for eager-loaded relations. When JSON is empty (spec didn't wire it),
// falls back to a flat map of Columns.
func rowAsJSON(r Row) (string, error) {
	if len(r.JSON) > 0 {
		// Re-marshal through a map to pretty-print regardless of how
		// the source was serialized.
		var v any
		if err := json.Unmarshal(r.JSON, &v); err == nil {
			b, err := json.MarshalIndent(v, "", "  ")
			if err == nil {
				return string(b), nil
			}
		}
		return string(r.JSON), nil
	}
	out := make(map[string]string, len(r.Columns)+1)
	for k, v := range r.Columns {
		out[k] = v
	}
	if r.ID != "" {
		if _, has := out["id"]; !has {
			out["id"] = r.ID
		}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (t *tableView) copyFocusedRowJSON() {
	row, _ := t.table.GetSelection()
	if row < 1 || row-1 >= len(t.rows) {
		return
	}
	s, err := rowAsJSON(t.rows[row-1])
	if err != nil {
		t.host().updateStatus("json error: " + err.Error())
		return
	}
	copyToClipboard(t.host(), s, "row (json)")
}

func (b *browser) copyFocusedRowJSON() {
	idx := b.list.GetCurrentItem()
	if idx < 0 || idx >= len(b.rows) {
		return
	}
	s, err := rowAsJSON(b.rows[idx])
	if err != nil {
		b.updateStatus("json error: " + err.Error())
		return
	}
	copyToClipboard(b.host(), s, "row (json)")
}
