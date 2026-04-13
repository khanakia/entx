package apidash

import "github.com/khanakia/entx/testentgqlmulti/ent"

// Resolver carries the ent.Client for all generated resolvers.
type Resolver struct {
	Client *ent.Client
}
