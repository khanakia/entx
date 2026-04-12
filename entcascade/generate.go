package entcascade

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"entgo.io/ent/entc/gen"
)

// generate analyses the graph, resolves template data, and writes cascade_delete.go.
func generate(g *gen.Graph) error {
	// Phase 1: Analyse — walk edges, build abstract delete operations.
	nodes := collectCascadeNodes(g)
	if len(nodes) == 0 {
		return nil
	}

	// Phase 2: Resolve — convert to template-friendly structs with precomputed strings.
	data := resolveData(g.Config.Package, nodes)

	// Phase 3: Render — execute Go template.
	var buf bytes.Buffer
	if err := cascadeTmpl.ExecuteTemplate(&buf, "file", data); err != nil {
		return fmt.Errorf("entcascade: execute template: %w", err)
	}

	// Phase 4: Format + write.
	outPath := filepath.Join(g.Config.Target, "cascade_delete.go")
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		_ = os.WriteFile(outPath, buf.Bytes(), 0644)
		return fmt.Errorf("entcascade: format generated code: %w", err)
	}
	return os.WriteFile(outPath, formatted, 0644)
}

// ══════════════════════════════════════════════════════════════════════════════
// Graph analysis
// ══════════════════════════════════════════════════════════════════════════════

type cascadeNode struct {
	name   string
	idType string
	ops    []deleteOp
}

type deleteOpKind int

const (
	opBulkDelete    deleteOpKind = iota // DELETE WHERE fk = id
	opNestedCascade                     // query IDs, delete children, then delete target
	opSoftDelete                        // UPDATE SET deleted_at = now WHERE fk = id
	opUnlink                            // UPDATE SET fk = NULL WHERE fk = id
)

type deleteOp struct {
	kind           deleteOpKind
	targetTypeName string // Go type to act on (e.g. "Message", "ChatbotUser")
	targetPkg      string // predicate package (e.g. "message")
	fkPredicate    string // predicate func name (e.g. "ChatbotID")
	comment        string
	childOps       []deleteOp // opNestedCascade only
	softDeleteField string    // opSoftDelete only: raw field name (e.g. "deleted_at")
}

// collectCascadeNodes returns cascade info for all annotated types.
func collectCascadeNodes(g *gen.Graph) []cascadeNode {
	typeMap := make(map[string]*gen.Type, len(g.Nodes))
	for _, n := range g.Nodes {
		typeMap[n.Name] = n
	}

	var nodes []cascadeNode
	for _, n := range g.Nodes {
		ann, ok := decodeAnnotation(n)
		if !ok {
			continue
		}
		ops := buildOps(n, ann, typeMap, map[string]bool{n.Name: true})
		if len(ops) == 0 {
			continue
		}
		nodes = append(nodes, cascadeNode{
			name:   n.Name,
			idType: n.ID.Type.String(),
			ops:    ops,
		})
	}
	return nodes
}

// buildOps creates ordered delete operations for an annotated type.
func buildOps(t *gen.Type, ann *Annotation, typeMap map[string]*gen.Type, visited map[string]bool) []deleteOp {
	skipSet := toSet(ann.SkipEdges)
	unlinkSet := toSet(ann.UnlinkEdges)
	hardDeleteSet := toSet(ann.HardDeleteEdges)
	softDeleteMap := make(map[string]string)
	for _, sd := range ann.SoftDeleteEdges {
		softDeleteMap[sd.Edge] = sd.Field
	}

	var ops []deleteOp
	for _, e := range t.Edges {
		if e.IsInverse() || skipSet[e.Name] || len(e.Rel.Columns) == 0 {
			continue
		}

		if e.M2M() {
			ops = append(ops, buildM2MOp(*e))
			continue
		}
		if !(e.O2M() || (e.O2O() && !e.OwnFK())) {
			continue
		}

		// O2M or O2O with FK on child — determine mode.
		switch {
		case unlinkSet[e.Name]:
			ops = append(ops, buildUnlinkOp(*e))
		case softDeleteMap[e.Name] != "":
			ops = append(ops, buildSoftDeleteOp(*e, softDeleteMap[e.Name]))
		case hardDeleteSet[e.Name]:
			ops = append(ops, buildDirectOp(*e, typeMap, visited))
		default:
			// Auto-detect: if target has deleted_at field, soft delete.
			if field, found := detectSoftDeleteField(e.Type); found {
				ops = append(ops, buildSoftDeleteOp(*e, field))
			} else {
				ops = append(ops, buildDirectOp(*e, typeMap, visited))
			}
		}
	}
	return ops
}

// buildM2MOp handles M2M edges — hard-deletes junction table rows.
func buildM2MOp(e gen.Edge) deleteOp {
	if e.Through != nil {
		return deleteOp{
			kind:           opBulkDelete,
			targetTypeName: e.Through.Name,
			targetPkg:      strings.ToLower(e.Through.Name),
			fkPredicate:    columnToPascal(e.Rel.Columns[0]),
			comment:        fmt.Sprintf("%s (M2M through %s)", e.Name, e.Through.Name),
		}
	}
	return deleteOp{
		kind:    opBulkDelete,
		comment: fmt.Sprintf("SKIP: %s — pure M2M without Through type", e.Name),
	}
}

// buildDirectOp handles O2M/O2O hard delete — recurses if target has children.
func buildDirectOp(e gen.Edge, typeMap map[string]*gen.Type, visited map[string]bool) deleteOp {
	target := e.Type
	pred := columnToPascal(e.Rel.Columns[0])

	childVisited := copyVisited(visited)
	childVisited[target.Name] = true

	childOps := buildChildOps(target, typeMap, childVisited)
	if len(childOps) > 0 {
		return deleteOp{
			kind:           opNestedCascade,
			targetTypeName: target.Name,
			targetPkg:      strings.ToLower(target.Name),
			fkPredicate:    pred,
			comment:        fmt.Sprintf("%s (cascade)", e.Name),
			childOps:       childOps,
		}
	}
	return deleteOp{
		kind:           opBulkDelete,
		targetTypeName: target.Name,
		targetPkg:      strings.ToLower(target.Name),
		fkPredicate:    pred,
		comment:        fmt.Sprintf("%s (O2M leaf)", e.Name),
	}
}

// buildSoftDeleteOp creates a soft-delete operation (UPDATE SET field = now).
// Soft-deleted targets are leaf operations — no recursion into their children.
func buildSoftDeleteOp(e gen.Edge, field string) deleteOp {
	return deleteOp{
		kind:            opSoftDelete,
		targetTypeName:  e.Type.Name,
		targetPkg:       strings.ToLower(e.Type.Name),
		fkPredicate:     columnToPascal(e.Rel.Columns[0]),
		comment:         fmt.Sprintf("%s (soft-delete via %s)", e.Name, field),
		softDeleteField: field,
	}
}

// buildUnlinkOp creates an unlink operation (UPDATE SET fk = NULL).
// Target entity survives; only the FK reference is cleared.
func buildUnlinkOp(e gen.Edge) deleteOp {
	return deleteOp{
		kind:           opUnlink,
		targetTypeName: e.Type.Name,
		targetPkg:      strings.ToLower(e.Type.Name),
		fkPredicate:    columnToPascal(e.Rel.Columns[0]),
		comment:        fmt.Sprintf("%s (unlink)", e.Name),
	}
}

// buildChildOps returns delete ops for a target type's own children (recursive).
// Uses auto-detection for soft delete; no annotation overrides at child level.
func buildChildOps(t *gen.Type, typeMap map[string]*gen.Type, visited map[string]bool) []deleteOp {
	var ops []deleteOp
	for _, e := range t.Edges {
		if e.IsInverse() || len(e.Rel.Columns) == 0 {
			continue
		}
		if visited[e.Type.Name] && !e.M2M() {
			continue
		}

		if e.M2M() {
			op := buildM2MOp(*e)
			if op.targetTypeName != "" {
				ops = append(ops, op)
			}
			continue
		}
		if !(e.O2M() || (e.O2O() && !e.OwnFK())) {
			continue
		}

		target := e.Type
		if visited[target.Name] {
			continue
		}

		// Auto-detect soft delete on child targets.
		if field, found := detectSoftDeleteField(target); found {
			ops = append(ops, buildSoftDeleteOp(*e, field))
			continue
		}

		childVisited := copyVisited(visited)
		childVisited[target.Name] = true

		grandchildOps := buildChildOps(target, typeMap, childVisited)
		kind := opBulkDelete
		comment := fmt.Sprintf("%s.%s (leaf)", t.Name, e.Name)
		if len(grandchildOps) > 0 {
			kind = opNestedCascade
			comment = fmt.Sprintf("%s.%s (cascade)", t.Name, e.Name)
		}
		ops = append(ops, deleteOp{
			kind:           kind,
			targetTypeName: target.Name,
			targetPkg:      strings.ToLower(target.Name),
			fkPredicate:    columnToPascal(e.Rel.Columns[0]),
			comment:        comment,
			childOps:       grandchildOps,
		})
	}
	return ops
}

// detectSoftDeleteField checks if a type has a "deleted_at" field (convention-based).
func detectSoftDeleteField(t *gen.Type) (string, bool) {
	for _, f := range t.Fields {
		if f.Name == "deleted_at" {
			return "deleted_at", true
		}
	}
	return "", false
}

// ══════════════════════════════════════════════════════════════════════════════
// Resolve — convert deleteOps into template-ready tmplOps
// ══════════════════════════════════════════════════════════════════════════════

func resolveData(pkgPath string, nodes []cascadeNode) tmplData {
	pkgs := make(map[string]bool)
	needsTime := false
	var tmplNodes []tmplNode

	for _, n := range nodes {
		singleTracker := newNameTracker()
		ops := resolveOps(n.ops, "id", false, singleTracker, pkgs, &needsTime)

		batchTracker := newNameTracker()
		batchOps := resolveOps(n.ops, "ids", true, batchTracker, pkgs, &needsTime)

		selfPkg := strings.ToLower(n.name)
		pkgs[selfPkg] = true

		tmplNodes = append(tmplNodes, tmplNode{
			Name:      n.name,
			IDType:    n.idType,
			CamelName: camelName(n.name),
			PkgName:   strings.ToLower(n.name),
			Ops:       ops,
			BatchOps:  batchOps,
		})
	}

	return tmplData{
		Package:     filepath.Base(pkgPath),
		ImportBlock: buildImportBlock(pkgs, pkgPath, needsTime),
		Nodes:       tmplNodes,
	}
}

func resolveOps(ops []deleteOp, parentVar string, parentSlice bool, tracker *nameTracker, pkgs map[string]bool, needsTime *bool) []tmplOp {
	var out []tmplOp
	for _, op := range ops {
		if op.targetTypeName == "" {
			out = append(out, tmplOp{IsSkip: true, Comment: op.comment})
			continue
		}

		pkgs[op.targetPkg] = true

		switch op.kind {
		case opBulkDelete:
			out = append(out, tmplOp{
				IsBulk:    true,
				Comment:   op.comment,
				TypeName:  op.targetTypeName,
				Predicate: predStr(op.targetPkg, op.fkPredicate, parentVar, parentSlice),
				CamelType: camelName(op.targetTypeName),
			})

		case opNestedCascade:
			varName := tracker.next(op.targetTypeName)
			queryPred := predStr(op.targetPkg, op.fkPredicate, parentVar, parentSlice)
			childOps := resolveOps(op.childOps, varName, true, tracker, pkgs, needsTime)
			out = append(out, tmplOp{
				IsNested:   true,
				Comment:    op.comment,
				VarName:    varName,
				QueryType:  op.targetTypeName,
				QueryPred:  queryPred,
				DeleteType: op.targetTypeName,
				DeletePred: queryPred,
				CamelQuery: camelName(op.targetTypeName),
				ChildOps:   childOps,
			})

		case opSoftDelete:
			*needsTime = true
			out = append(out, tmplOp{
				IsSoftDelete:     true,
				Comment:          op.comment,
				TypeName:         op.targetTypeName,
				Predicate:        predStr(op.targetPkg, op.fkPredicate, parentVar, parentSlice),
				CamelType:        camelName(op.targetTypeName),
				SoftDeleteSetter: "Set" + columnToPascal(op.softDeleteField),
			})

		case opUnlink:
			out = append(out, tmplOp{
				IsUnlink:    true,
				Comment:     op.comment,
				TypeName:    op.targetTypeName,
				Predicate:   predStr(op.targetPkg, op.fkPredicate, parentVar, parentSlice),
				CamelType:   camelName(op.targetTypeName),
				UnlinkClear: "Clear" + op.fkPredicate,
			})
		}
	}
	return out
}

func predStr(pkg, predFunc, varName string, isSlice bool) string {
	if isSlice {
		return fmt.Sprintf("%s.%sIn(%s...)", pkg, predFunc, varName)
	}
	return fmt.Sprintf("%s.%s(%s)", pkg, predFunc, varName)
}

func buildImportBlock(pkgs map[string]bool, pkgPath string, needsTime bool) string {
	var buf strings.Builder
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"fmt\"\n")
	if needsTime {
		buf.WriteString("\t\"time\"\n")
	}

	var sorted []string
	for pkg := range pkgs {
		sorted = append(sorted, pkgPath+"/"+pkg)
	}
	sortStrings(sorted)

	if len(sorted) > 0 {
		buf.WriteString("\n")
		for _, imp := range sorted {
			fmt.Fprintf(&buf, "\t\"%s\"\n", imp)
		}
	}
	buf.WriteString(")")
	return buf.String()
}

// ══════════════════════════════════════════════════════════════════════════════
// Name tracker
// ══════════════════════════════════════════════════════════════════════════════

type nameTracker struct {
	counts map[string]int
}

func newNameTracker() *nameTracker {
	return &nameTracker{counts: make(map[string]int)}
}

func (nt *nameTracker) next(typeName string) string {
	base := camelName(typeName) + "IDs"
	nt.counts[base]++
	if nt.counts[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s%d", base, nt.counts[base])
}

// ══════════════════════════════════════════════════════════════════════════════
// Helpers
// ══════════════════════════════════════════════════════════════════════════════

func columnToPascal(col string) string {
	acronyms := map[string]string{
		"id": "ID", "url": "URL", "api": "API", "ip": "IP",
		"uri": "URI", "sql": "SQL", "http": "HTTP", "uuid": "UUID",
	}
	parts := strings.Split(col, "_")
	for i, p := range parts {
		lower := strings.ToLower(p)
		if acronym, ok := acronyms[lower]; ok {
			parts[i] = acronym
		} else if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func camelName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func copyVisited(m map[string]bool) map[string]bool {
	cp := make(map[string]bool, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
