package sqlite

import "errors"

// ErrNoRows is returned by Row.Scan when the query returns no rows.
var ErrNoRows = errors.New("sqlite: no rows")

// SQLite primary result codes.
const (
	CodeConstraint = 19 // SQLITE_CONSTRAINT
)

// SQLite extended result codes for constraint violations.
const (
	CodeConstraintCheck      = 275  // SQLITE_CONSTRAINT_CHECK
	CodeConstraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	CodeConstraintNotNull    = 1299 // SQLITE_CONSTRAINT_NOTNULL
	CodeConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	CodeConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// Error represents a SQLite error with result code information. Use
// errors.As to extract it from wrapped errors, or use the Is* helper
// functions for common checks.
type Error struct {
	// Code is the primary result code (e.g., 19 for SQLITE_CONSTRAINT).
	Code int
	// ExtendedCode is the extended result code that distinguishes
	// subtypes (e.g., 2067 for SQLITE_CONSTRAINT_UNIQUE).
	ExtendedCode int
	// Message is the human-readable error message from SQLite.
	Message string
}

// Error returns the error message.
func (e *Error) Error() string {
	return e.Message
}

// IsConstraintError reports whether err is any SQLite constraint
// violation (UNIQUE, FOREIGN KEY, NOT NULL, CHECK, PRIMARY KEY).
func IsConstraintError(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == CodeConstraint
}

// IsUniqueConstraintError reports whether err is a UNIQUE constraint
// violation. This is the most common constraint error — it occurs when
// an INSERT or UPDATE would create a duplicate value in a column with
// a UNIQUE index.
func IsUniqueConstraintError(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.ExtendedCode == CodeConstraintUnique
}

// IsForeignKeyConstraintError reports whether err is a FOREIGN KEY
// constraint violation.
func IsForeignKeyConstraintError(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.ExtendedCode == CodeConstraintForeignKey
}

// IsNotNullConstraintError reports whether err is a NOT NULL constraint
// violation.
func IsNotNullConstraintError(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.ExtendedCode == CodeConstraintNotNull
}
