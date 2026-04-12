package entgqlmulti

// Config holds configuration for the multi-API schema generator.
type Config struct {
	// apiOutputPaths maps API name to the output file path for the generated ent.graphql.
	// Example: {"apiadmin": "../apiadmin/internal/graph/schemas/ent.graphql"}
	apiOutputPaths map[string]string

	// defaultAPI is the API name used for entities that have no ApiConfig annotation.
	// Those entities will only appear in this API's schema.
	// Defaults to "apidash".
	defaultAPI string

	// entPackage is the Go import path for Ent generated types.
	// Used in @goModel directives for subset types.
	// Example: "dbent/gen/ent"
	entPackage string
}

// Option configures the Generator.
type Option func(*Config)

// WithAPIOutputPath registers an output path for an API.
// Only APIs with registered output paths will have schemas generated.
func WithAPIOutputPath(apiName, path string) Option {
	return func(c *Config) {
		if c.apiOutputPaths == nil {
			c.apiOutputPaths = make(map[string]string)
		}
		c.apiOutputPaths[apiName] = path
	}
}

// WithDefaultAPI sets which API receives entities that have no ApiConfig annotation.
// Defaults to "apidash".
func WithDefaultAPI(name string) Option {
	return func(c *Config) {
		c.defaultAPI = name
	}
}

// WithEntPackage sets the Go import path for Ent generated types.
// This is used in @goModel directives for subset types (types with custom TypeName).
// Example: "dbent/gen/ent"
func WithEntPackage(pkg string) Option {
	return func(c *Config) {
		c.entPackage = pkg
	}
}
