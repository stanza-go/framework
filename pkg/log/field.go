package log

import "time"

// Field is a key-value pair attached to a log entry.
type Field struct {
	Key   string
	Value any
}

// String returns a Field with a string value.
func String(key, val string) Field {
	return Field{Key: key, Value: val}
}

// Int returns a Field with an int value.
func Int(key string, val int) Field {
	return Field{Key: key, Value: val}
}

// Int64 returns a Field with an int64 value.
func Int64(key string, val int64) Field {
	return Field{Key: key, Value: val}
}

// Float64 returns a Field with a float64 value.
func Float64(key string, val float64) Field {
	return Field{Key: key, Value: val}
}

// Bool returns a Field with a bool value.
func Bool(key string, val bool) Field {
	return Field{Key: key, Value: val}
}

// Err returns a Field with key "error" and the error's message as value.
// If err is nil, the value is nil.
func Err(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

// Duration returns a Field with a time.Duration value formatted as a string.
func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Value: val.String()}
}

// Time returns a Field with a time.Time value formatted as RFC3339 in UTC.
func Time(key string, val time.Time) Field {
	return Field{Key: key, Value: val.UTC().Format(time.RFC3339)}
}

// Any returns a Field with an arbitrary value.
func Any(key string, val any) Field {
	return Field{Key: key, Value: val}
}
