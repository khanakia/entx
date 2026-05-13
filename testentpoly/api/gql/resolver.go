package gql

import "github.com/khanakia/entx/testentpoly/ent"

// Resolver carries the ent.Client used by every generated resolver.
type Resolver struct {
	Client *ent.Client
}
