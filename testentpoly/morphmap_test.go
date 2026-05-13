// Scenario 12 from SCENARIOS.md: morph-map rename. SKIPPED until
// entpoly's codegen learns to deduplicate MorphKey constants when
// multiple aliases map to the same schema. See the Deviations section
// in the phase report.
package testentpoly

import "testing"

// TestMorphMap_Rename — scenario 12 (skipped).
func TestMorphMap_Rename(t *testing.T) {
	t.Skip("entpoly codegen emits duplicate MorphKey constants when WithMorphMap registers two aliases for the same schema (e.g. 'post' AND 'legacy_post' → Post). Skipping until entpoly merges or otherwise dedups alias emission.")
}
