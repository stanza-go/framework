package log

import "strings"

// Level represents the severity of a log entry.
type Level int8

const (
	// LevelDebug is for verbose development information.
	LevelDebug Level = iota
	// LevelInfo is for general operational information.
	LevelInfo
	// LevelWarn is for conditions that may need attention.
	LevelWarn
	// LevelError is for errors that need investigation.
	LevelError
)

var levelNames = [...]string{"debug", "info", "warn", "error"}

// String returns the lowercase name of the level.
func (l Level) String() string {
	if l >= LevelDebug && l <= LevelError {
		return levelNames[l]
	}
	return "unknown"
}

// ParseLevel converts a string to a Level. Returns LevelInfo for unrecognized strings.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error", "err":
		return LevelError
	default:
		return LevelInfo
	}
}
