package entgqlmulti

import (
	"fmt"
	"os"
	"strings"

	"entgo.io/ent/entc/gen"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
)

// Generator produces per-API GraphQL schema files from Ent schema annotations.
type Generator struct {
	config *Config
}

// New creates a Generator with the given options.
func New(opts ...Option) *Generator {
	cfg := &Config{
		defaultAPI:     "apidash",
		apiOutputPaths: make(map[string]string),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Generator{config: cfg}
}

// entityWork represents a single type generation task for an API.
type entityWork struct {
	entName string    // Original Ent entity name (e.g., "Chatbot")
	gqlName string    // GraphQL type name in the full schema (usually same as entName)
	target  ApiTarget // The specific target config
}

// SchemaHook returns a function compatible with entgql.WithSchemaHook().
// It reads entity annotations from gen.Graph and the full ast.Schema,
// then generates filtered per-API schemas.
func (g *Generator) SchemaHook() func(*gen.Graph, *ast.Schema) error {
	return func(graph *gen.Graph, fullSchema *ast.Schema) error {
		// Step 1: Build entity index — map[apiName][]entityWork
		apiWork := make(map[string][]entityWork)

		for _, node := range graph.Nodes {
			ann, err := decodeApiConfig(node.Annotations)
			if err != nil {
				return fmt.Errorf("entgqlmulti: node %s: %w", node.Name, err)
			}

			if ann == nil {
				// No annotation — assign to default API with full access.
				// The default API is already handled by entgql's built-in WithSchemaPath,
				// so we don't need to generate it here unless it has an explicit output path.
				continue
			}

			for apiName, targets := range ann.Targets {
				for _, target := range targets {
					apiWork[apiName] = append(apiWork[apiName], entityWork{
						entName: node.Name,
						gqlName: node.Name, // entgql uses the same name by default
						target:  target,
					})
				}
			}
		}

		// Step 2: Generate schema for each API with a configured output path
		for apiName, outputPath := range g.config.apiOutputPaths {
			work, ok := apiWork[apiName]
			if !ok {
				// No entities annotated for this API — skip
				continue
			}

			schema, err := g.buildApiSchema(fullSchema, work)
			if err != nil {
				return fmt.Errorf("entgqlmulti: api %s: %w", apiName, err)
			}

			output := formatSchema(schema)
			if err := os.WriteFile(outputPath, []byte(output), 0644); err != nil {
				return fmt.Errorf("entgqlmulti: write %s: %w", outputPath, err)
			}
		}

		return nil
	}
}

// buildApiSchema creates a filtered ast.Schema for a specific API.
func (g *Generator) buildApiSchema(fullSchema *ast.Schema, work []entityWork) (*ast.Schema, error) {
	s := &ast.Schema{
		Types:      make(map[string]*ast.Definition),
		Directives: make(map[string]*ast.DirectiveDefinition),
	}

	// Add directive definitions
	for name, d := range fullSchema.Directives {
		s.Directives[name] = copyDirectiveDefinition(d)
	}

	// Add common built-in types
	addCommonTypes(s, fullSchema)

	// Build set of all entity type names present in this API (for edge filtering)
	presentTypes := make(map[string]bool)
	for _, w := range work {
		typeName := w.target.TypeName
		if typeName == "" {
			typeName = w.gqlName
		}
		presentTypes[typeName] = true
	}

	// Track which scalars/enums are needed
	neededTypes := make(map[string]bool)

	var queryFields ast.FieldList
	var mutationFields ast.FieldList

	for _, w := range work {
		typeName := w.target.TypeName
		if typeName == "" {
			typeName = w.gqlName
		}

		// Get the source type definition from the full schema
		srcDef := fullSchema.Types[w.gqlName]
		if srcDef == nil {
			return nil, fmt.Errorf("type %q not found in full schema", w.gqlName)
		}

		// Copy and filter the entity type
		entityDef := copyDefinition(srcDef)
		entityDef.Name = typeName

		// Remove "Node" interface implementation. The Node interface requires
		// a node(id) query resolver with a type switch. When multiple types map
		// to the same Go struct (subset types), this causes duplicate cases.
		// Non-default APIs typically don't need the Node relay resolver.
		entityDef.Interfaces = nil

		// Filter fields if specified
		if len(w.target.Fields) > 0 {
			entityDef.Fields = filterFields(entityDef.Fields, w.target.Fields)
		} else {
			// All fields — but filter out edge fields referencing absent types
			entityDef.Fields = filterEdgeFields(entityDef.Fields, presentTypes, fullSchema)
		}

		// Add @goModel directive if using a custom type name
		if w.target.TypeName != "" && g.config.entPackage != "" {
			entityDef.Directives = append(entityDef.Directives, goModelDirective(
				g.config.entPackage+"."+w.entName,
			))
		}

		// Track referenced types (scalars, enums)
		for _, field := range entityDef.Fields {
			trackReferencedType(field.Type, neededTypes)
		}

		s.AddTypes(entityDef)

		// Generate Connection/Edge if Query is true
		if w.target.Query {
			connDef, edgeDef := buildConnectionTypes(typeName)
			s.AddTypes(connDef, edgeDef)

			// Build query field
			qf := buildQueryField(typeName, w)

			// Add OrderBy arg if requested
			if w.target.OrderBy {
				orderName := orderTypeName(w.gqlName)
				orderFieldName := orderFieldEnumName(w.gqlName)

				// Copy Order input type from full schema
				if orderDef := fullSchema.Types[orderName]; orderDef != nil {
					s.AddTypes(copyDefinition(orderDef))
					neededTypes["OrderDirection"] = true
				}
				// Copy OrderField enum from full schema
				if enumDef := fullSchema.Types[orderFieldName]; enumDef != nil {
					s.AddTypes(copyDefinition(enumDef))
				}

				qf.Arguments = append(qf.Arguments, &ast.ArgumentDefinition{
					Description: fmt.Sprintf("Ordering options for %s returned from the connection.", plural(typeName)),
					Name:        "orderBy",
					Type:        ast.ListType(ast.NonNullNamedType(orderName, nil), nil),
				})
			}

			// Add Filters arg if requested
			if w.target.Filters {
				whereName := whereInputName(w.gqlName)
				if whereDef := fullSchema.Types[whereName]; whereDef != nil {
					filteredWhere := copyDefinition(whereDef)
					// If fields are restricted, filter the WhereInput too
					if len(w.target.Fields) > 0 {
						filteredWhere.Fields = filterWhereInputFields(filteredWhere.Fields, w.target.Fields)
					}
					s.AddTypes(filteredWhere)
					// Track types referenced by WhereInput fields
					for _, f := range filteredWhere.Fields {
						trackReferencedType(f.Type, neededTypes)
					}
				}

				qf.Arguments = append(qf.Arguments, &ast.ArgumentDefinition{
					Description: fmt.Sprintf("Filtering options for %s returned from the connection.", plural(typeName)),
					Name:        "where",
					Type:        ast.NamedType(whereName, nil),
				})
			}

			queryFields = append(queryFields, qf)
		}

		// Generate mutations if requested
		if w.target.Mutations {
			createName := createInputName(w.gqlName)
			updateName := updateInputName(w.gqlName)

			if createDef := fullSchema.Types[createName]; createDef != nil {
				inputCopy := copyDefinition(createDef)
				if len(w.target.Fields) > 0 {
					inputCopy.Fields = filterMutationInputFields(inputCopy.Fields, w.target.Fields)
				}
				s.AddTypes(inputCopy)
				for _, f := range inputCopy.Fields {
					trackReferencedType(f.Type, neededTypes)
				}

				mutationFields = append(mutationFields, &ast.FieldDefinition{
					Description: fmt.Sprintf("Creates a new %s.", typeName),
					Name:        "create" + w.gqlName,
					Arguments: ast.ArgumentDefinitionList{
						{
							Name: "input",
							Type: ast.NonNullNamedType(createName, nil),
						},
					},
					Type: ast.NonNullNamedType(typeName, nil),
				})
			}

			if updateDef := fullSchema.Types[updateName]; updateDef != nil {
				inputCopy := copyDefinition(updateDef)
				if len(w.target.Fields) > 0 {
					inputCopy.Fields = filterMutationInputFields(inputCopy.Fields, w.target.Fields)
				}
				s.AddTypes(inputCopy)
				for _, f := range inputCopy.Fields {
					trackReferencedType(f.Type, neededTypes)
				}

				mutationFields = append(mutationFields, &ast.FieldDefinition{
					Description: fmt.Sprintf("Updates an existing %s.", typeName),
					Name:        "update" + w.gqlName,
					Arguments: ast.ArgumentDefinitionList{
						{
							Name: "id",
							Type: ast.NonNullNamedType("ID", nil),
						},
						{
							Name: "input",
							Type: ast.NonNullNamedType(updateName, nil),
						},
					},
					Type: ast.NonNullNamedType(typeName, nil),
				})
			}
		}
	}

	// Add needed scalar/enum types from full schema
	for typeName := range neededTypes {
		if s.Types[typeName] != nil {
			continue // already added
		}
		if srcDef := fullSchema.Types[typeName]; srcDef != nil {
			if srcDef.Kind == ast.Scalar || srcDef.Kind == ast.Enum {
				cp := copyDefinition(srcDef)
				cp.BuiltIn = false
				s.AddTypes(cp)
			}
		} else {
			// Type not in fullSchema (e.g., custom scalars defined in gqlgen config
			// like ItemGroupSourceType, ItemGroupSourceTrainingStatus).
			// Add as scalar so the schema is self-contained.
			s.AddTypes(&ast.Definition{
				Kind: ast.Scalar,
				Name: typeName,
			})
		}
	}

	// Assemble Query type
	if len(queryFields) > 0 {
		queryDef := &ast.Definition{
			Kind:   ast.Object,
			Name:   "Query",
			Fields: queryFields,
		}
		s.AddTypes(queryDef)
		s.Query = s.Types["Query"]
	}

	// Assemble Mutation type
	if len(mutationFields) > 0 {
		mutDef := &ast.Definition{
			Kind:   ast.Object,
			Name:   "Mutation",
			Fields: mutationFields,
		}
		s.AddTypes(mutDef)
		s.Mutation = s.Types["Mutation"]
	}

	return s, nil
}

// addCommonTypes adds scalars, PageInfo, Cursor, OrderDirection, and Node interface.
func addCommonTypes(s *ast.Schema, fullSchema *ast.Schema) {
	commonTypeNames := []string{
		"Cursor", "PageInfo", "OrderDirection", "Node",
	}
	for _, name := range commonTypeNames {
		if def := fullSchema.Types[name]; def != nil {
			cp := copyDefinition(def)
			// Ensure BuiltIn is false so the formatter includes these types in the output.
			// entgql marks some types as BuiltIn which causes the formatter to skip them.
			cp.BuiltIn = false
			s.AddTypes(cp)
		}
	}

	// Time scalar may not be in fullSchema.Types (it's a built-in that entgql
	// doesn't add to the Types map). Add it explicitly if not already present.
	if s.Types["Time"] == nil {
		s.AddTypes(&ast.Definition{
			Kind:        ast.Scalar,
			Name:        "Time",
			Description: "The builtin Time type",
		})
	}
}

// normalizeFieldNames converts field names to camelCase for matching against GraphQL field names.
// Accepts both snake_case (Ent constants like chatbot.FieldCreatedAt = "created_at")
// and camelCase ("createdAt"). Returns a set of camelCase names.
func normalizeFieldNames(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[camel(name)] = true
	}
	return set
}

// filterFields keeps only fields whose names are in the allowed list.
// Allowed names can be snake_case (Ent constants) or camelCase (GraphQL names).
func filterFields(fields ast.FieldList, allowed []string) ast.FieldList {
	set := normalizeFieldNames(allowed)
	var result ast.FieldList
	for _, f := range fields {
		if set[f.Name] {
			result = append(result, f)
		}
	}
	return result
}

// filterEdgeFields removes fields that reference types not present in the API.
// It keeps primitive fields and edge fields whose target type is in presentTypes.
func filterEdgeFields(fields ast.FieldList, presentTypes map[string]bool, fullSchema *ast.Schema) ast.FieldList {
	var result ast.FieldList
	for _, f := range fields {
		typeName := resolveTypeName(f.Type)
		// Check if this is a complex type that should be filtered
		if typeName != "" {
			targetDef := fullSchema.Types[typeName]
			if targetDef != nil && targetDef.Kind == ast.Object && !presentTypes[typeName] {
				// This field references an entity type not in this API — skip
				// But keep Connection types (they'll be generated)
				if !strings.HasSuffix(typeName, "Connection") {
					continue
				}
			}
		}
		result = append(result, f)
	}
	return result
}

// filterWhereInputFields filters WhereInput fields to match restricted entity fields.
// Keeps: logical operators (not, and, or), fields matching the allowed entity field names,
// and edge predicate fields (has*, hasWith).
// Allowed names can be snake_case or camelCase.
func filterWhereInputFields(fields ast.FieldList, allowedEntityFields []string) ast.FieldList {
	// Normalize to camelCase for matching
	normalizedNames := make([]string, len(allowedEntityFields))
	for i, name := range allowedEntityFields {
		normalizedNames[i] = camel(name)
	}
	allowed := make(map[string]bool, len(normalizedNames))
	for _, name := range normalizedNames {
		allowed[name] = true
	}

	var result ast.FieldList
	for _, f := range fields {
		// Always keep logical operators
		if f.Name == "not" || f.Name == "and" || f.Name == "or" {
			result = append(result, f)
			continue
		}
		// Always keep id-related predicates
		if f.Name == "id" || strings.HasPrefix(f.Name, "id") {
			if allowed["id"] {
				result = append(result, f)
			}
			continue
		}
		// Always keep edge predicates (has*, hasWith)
		if strings.HasPrefix(f.Name, "has") {
			result = append(result, f)
			continue
		}
		// Match field predicates by camelCase prefix
		// e.g., "nameContains" matches entity field "name"
		matched := false
		for _, cf := range normalizedNames {
			if f.Name == cf || strings.HasPrefix(f.Name, cf) {
				matched = true
				break
			}
		}
		if matched {
			result = append(result, f)
		}
	}
	return result
}

// filterMutationInputFields filters Create/Update input fields to match restricted entity fields.
// Keeps fields whose camelCase name matches any allowed entity field.
// Allowed names can be snake_case or camelCase.
func filterMutationInputFields(fields ast.FieldList, allowedEntityFields []string) ast.FieldList {
	set := normalizeFieldNames(allowedEntityFields)
	var result ast.FieldList
	for _, f := range fields {
		if set[f.Name] {
			result = append(result, f)
		}
	}
	return result
}

// buildConnectionTypes creates the Connection and Edge type definitions.
func buildConnectionTypes(typeName string) (*ast.Definition, *ast.Definition) {
	connDef := &ast.Definition{
		Kind:        ast.Object,
		Description: "A connection to a list of items.",
		Name:        connectionTypeName(typeName),
		Fields: ast.FieldList{
			{
				Description: "A list of edges.",
				Name:        "edges",
				Type:        ast.ListType(ast.NamedType(edgeTypeName(typeName), nil), nil),
			},
			{
				Description: "Information to aid in pagination.",
				Name:        "pageInfo",
				Type:        ast.NonNullNamedType("PageInfo", nil),
			},
			{
				Description: "Identifies the total count of items in the connection.",
				Name:        "totalCount",
				Type:        ast.NonNullNamedType("Int", nil),
			},
		},
	}

	edgeDef := &ast.Definition{
		Kind:        ast.Object,
		Description: "An edge in a connection.",
		Name:        edgeTypeName(typeName),
		Fields: ast.FieldList{
			{
				Description: "The item at the end of the edge.",
				Name:        "node",
				Type:        ast.NamedType(typeName, nil),
			},
			{
				Description: "A cursor for use in pagination.",
				Name:        "cursor",
				Type:        ast.NonNullNamedType("Cursor", nil),
			},
		},
	}

	return connDef, edgeDef
}

// buildQueryField creates the Query field definition for a connection query.
func buildQueryField(typeName string, w entityWork) *ast.FieldDefinition {
	qName := w.target.QueryName
	if qName == "" {
		qName = queryFieldName(typeName)
	}

	return &ast.FieldDefinition{
		Name: qName,
		Arguments: ast.ArgumentDefinitionList{
			{
				Description: "Returns the elements in the list that come after the specified cursor.",
				Name:        "after",
				Type:        ast.NamedType("Cursor", nil),
			},
			{
				Description: "Returns the first _n_ elements from the list.",
				Name:        "first",
				Type:        ast.NamedType("Int", nil),
			},
			{
				Description: "Returns the elements in the list that come before the specified cursor.",
				Name:        "before",
				Type:        ast.NamedType("Cursor", nil),
			},
			{
				Description: "Returns the last _n_ elements from the list.",
				Name:        "last",
				Type:        ast.NamedType("Int", nil),
			},
		},
		Type: ast.NonNullNamedType(connectionTypeName(typeName), nil),
	}
}

// goModelDirective creates a @goModel(model: "pkg.Type") directive.
func goModelDirective(model string) *ast.Directive {
	return &ast.Directive{
		Name: "goModel",
		Arguments: ast.ArgumentList{
			{
				Name: "model",
				Value: &ast.Value{
					Kind: ast.StringValue,
					Raw:  model,
				},
			},
		},
	}
}

// resolveTypeName extracts the base named type from an ast.Type (unwrapping lists and non-null).
func resolveTypeName(t *ast.Type) string {
	if t == nil {
		return ""
	}
	if t.NamedType != "" {
		return t.NamedType
	}
	return resolveTypeName(t.Elem)
}

// trackReferencedType adds the base type name to the neededTypes set.
// Skips built-in scalars (String, Int, Boolean, Float, ID).
func trackReferencedType(t *ast.Type, neededTypes map[string]bool) {
	name := resolveTypeName(t)
	if name == "" {
		return
	}
	// Skip GraphQL built-in scalars
	switch name {
	case "String", "Int", "Float", "Boolean", "ID":
		return
	}
	neededTypes[name] = true
}

// formatSchema renders an ast.Schema to a GraphQL SDL string.
func formatSchema(s *ast.Schema) string {
	sb := &strings.Builder{}
	f := formatter.NewFormatter(sb, formatter.WithIndent("  "))
	f.FormatSchema(s)
	return sb.String()
}
