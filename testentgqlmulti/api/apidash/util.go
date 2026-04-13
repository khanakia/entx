package apidash

import "strconv"

// parseID converts a relay-style string ID into the int primary key used by ent.
func parseID(id string) (int, error) {
	return strconv.Atoi(id)
}
