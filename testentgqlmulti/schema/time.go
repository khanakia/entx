package schema

import "time"

// timeNow is extracted for testability — tests can override it if needed.
var timeNow = time.Now
