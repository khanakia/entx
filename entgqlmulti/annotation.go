package entgqlmulti

import (
	"encoding/json"
	"fmt"
)

// AnnotationName is the key used in Ent's Annotations map.
const AnnotationName = "EntGqlMulti"

// ApiTarget declares how an Ent entity participates in a specific API's GraphQL schema.
// Multiple ApiTargets per API are supported — each generates a separate GraphQL type
// from the same Ent entity (e.g., Chatbot + ChatbotGuest both backed by ent.Chatbot).
type ApiTarget struct {
	// TypeName overrides the GraphQL type name.
	// Empty = use the Ent entity name (e.g., "Chatbot").
	// Set to generate a subset type with a different name (e.g., "ChatbotGuest").
	// When set, a @goModel directive is added to bind to the original Ent struct.
	TypeName string `json:"TypeName,omitempty"`

	// Fields lists which fields to include. nil/empty = all fields.
	Fields []string `json:"Fields,omitempty"`

	// Query generates a root Query connection field for this entity.
	Query bool `json:"Query,omitempty"`

	// QueryName overrides the generated query field name.
	// Only used when Query is true. Empty = auto-derived from TypeName or entity name.
	QueryName string `json:"QueryName,omitempty"`

	// Mutations enables Create/Update mutations.
	Mutations bool `json:"Mutations,omitempty"`

	// Filters includes WhereInput argument on the query connection field.
	Filters bool `json:"Filters,omitempty"`

	// OrderBy includes OrderBy argument on the query connection field.
	OrderBy bool `json:"OrderBy,omitempty"`
}

// ApiConfigAnnotation holds per-API generation config for an Ent entity.
// Each API key maps to a slice of ApiTargets — one entry = one generated type.
// Multiple entries generate multiple types from the same Ent entity.
type ApiConfigAnnotation struct {
	Targets map[string][]ApiTarget `json:"Targets"`
}

// Name implements ent schema.Annotation.
func (ApiConfigAnnotation) Name() string {
	return AnnotationName
}

// ApiConfig creates the annotation for use in Ent schema Annotations().
// Entities without this annotation are only generated for the default API (backward compatible).
func ApiConfig(targets map[string][]ApiTarget) ApiConfigAnnotation {
	return ApiConfigAnnotation{Targets: targets}
}

// decodeApiConfig reads the annotation from gen.Type.Annotations map.
// Returns nil, nil if the annotation is absent.
func decodeApiConfig(annotations map[string]any) (*ApiConfigAnnotation, error) {
	raw, ok := annotations[AnnotationName]
	if !ok {
		return nil, nil
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("entgqlmulti: marshal annotation: %w", err)
	}
	var ann ApiConfigAnnotation
	if err := json.Unmarshal(buf, &ann); err != nil {
		return nil, fmt.Errorf("entgqlmulti: unmarshal annotation: %w", err)
	}
	return &ann, nil
}
