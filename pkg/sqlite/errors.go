package sqlite

import "errors"

// ErrNoRows is returned by Row.Scan when the query returns no rows.
var ErrNoRows = errors.New("sqlite: no rows")
