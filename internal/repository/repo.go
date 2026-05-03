package repository

import "time"

// dbCallTimeout caps every individual DB operation. context.WithTimeout can
// only shorten a deadline, never extend one — so callers that pass a stricter
// context (a 1s request deadline, say) still win.
const dbCallTimeout = 5 * time.Second
