package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// schedule represents a parsed cron expression. Each field is a sorted set of
// allowed values encoded as a bitset for O(1) matching.
type schedule struct {
	minute uint64 // bits 0-59
	hour   uint64 // bits 0-23
	dom    uint64 // bits 1-31
	month  uint64 // bits 1-12
	dow    uint64 // bits 0-6 (Sunday=0)
	expr   string // original expression for display
}

// parse parses a standard 5-field cron expression.
//
// Field order: minute hour day-of-month month day-of-week
//
// Supported syntax per field:
//   - *        every value
//   - N        specific value
//   - N-M      range (inclusive)
//   - */N      every N from min
//   - N-M/S    range with step
//   - N,M,O    list of values or sub-expressions
func parse(expr string) (schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return schedule{}, fmt.Errorf("cron: expected 5 fields, got %d in %q", len(fields), expr)
	}

	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return schedule{}, fmt.Errorf("cron: minute field: %w", err)
	}

	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return schedule{}, fmt.Errorf("cron: hour field: %w", err)
	}

	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return schedule{}, fmt.Errorf("cron: day-of-month field: %w", err)
	}

	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return schedule{}, fmt.Errorf("cron: month field: %w", err)
	}

	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return schedule{}, fmt.Errorf("cron: day-of-week field: %w", err)
	}

	return schedule{
		minute: minute,
		hour:   hour,
		dom:    dom,
		month:  month,
		dow:    dow,
		expr:   expr,
	}, nil
}

// matches returns true if the given time matches this schedule.
func (s schedule) matches(t time.Time) bool {
	return s.minute&(1<<uint(t.Minute())) != 0 &&
		s.hour&(1<<uint(t.Hour())) != 0 &&
		s.dom&(1<<uint(t.Day())) != 0 &&
		s.month&(1<<uint(t.Month())) != 0 &&
		s.dow&(1<<uint(t.Weekday())) != 0
}

// next returns the next time at or after the given time that matches this
// schedule. It searches up to 4 years ahead to handle leap-year edge cases.
// The returned time is always truncated to the minute.
func (s schedule) next(from time.Time) time.Time {
	// Start from the next whole minute.
	t := from.Truncate(time.Minute).Add(time.Minute)

	// Search limit: 4 years covers all possible cron patterns including
	// Feb 29 and rare month/dow combinations.
	limit := t.Add(4 * 365 * 24 * time.Hour)

	for t.Before(limit) {
		// Fast-forward month.
		if s.month&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Fast-forward day.
		if s.dom&(1<<uint(t.Day())) == 0 || s.dow&(1<<uint(t.Weekday())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Fast-forward hour.
		if s.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}

		// Fast-forward minute.
		if s.minute&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}

		return t
	}

	// Should never happen with valid schedules, but return zero if it does.
	return time.Time{}
}

// parseField parses a single cron field into a bitset of allowed values.
func parseField(field string, min, max int) (uint64, error) {
	var bits uint64

	for _, part := range strings.Split(field, ",") {
		b, err := parsePart(part, min, max)
		if err != nil {
			return 0, err
		}
		bits |= b
	}

	if bits == 0 {
		return 0, fmt.Errorf("empty result for field %q", field)
	}

	return bits, nil
}

// parsePart parses a single part of a cron field (between commas).
// Supported forms: *, N, N-M, */S, N-M/S
func parsePart(part string, min, max int) (uint64, error) {
	var bits uint64

	// Split on '/' for step.
	rangeStr, stepStr, hasStep := strings.Cut(part, "/")

	step := 1
	if hasStep {
		s, err := strconv.Atoi(stepStr)
		if err != nil || s <= 0 {
			return 0, fmt.Errorf("invalid step %q", stepStr)
		}
		step = s
	}

	if rangeStr == "*" {
		for i := min; i <= max; i += step {
			bits |= 1 << uint(i)
		}
		return bits, nil
	}

	// Split on '-' for range.
	lowStr, highStr, hasRange := strings.Cut(rangeStr, "-")

	low, err := strconv.Atoi(lowStr)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", lowStr)
	}
	if low < min || low > max {
		return 0, fmt.Errorf("value %d out of range [%d, %d]", low, min, max)
	}

	if !hasRange {
		if hasStep {
			// N/S — range from N to max with step S.
			for i := low; i <= max; i += step {
				bits |= 1 << uint(i)
			}
			return bits, nil
		}
		// Single value.
		return 1 << uint(low), nil
	}

	high, err := strconv.Atoi(highStr)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", highStr)
	}
	if high < min || high > max {
		return 0, fmt.Errorf("value %d out of range [%d, %d]", high, min, max)
	}
	if high < low {
		return 0, fmt.Errorf("invalid range %d-%d", low, high)
	}

	for i := low; i <= high; i += step {
		bits |= 1 << uint(i)
	}
	return bits, nil
}
