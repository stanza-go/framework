package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// entry is a helper for unmarshaling JSON log output.
type entry map[string]any

func parseEntry(t *testing.T, data []byte) entry {
	t.Helper()
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, data)
	}
	return e
}

func parseLines(t *testing.T, data []byte) []entry {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	entries := make([]entry, len(lines))
	for i, line := range lines {
		entries[i] = parseEntry(t, []byte(line))
	}
	return entries
}

// --- Level tests ---

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "debug"},
		{LevelInfo, "info"},
		{LevelWarn, "warn"},
		{LevelError, "error"},
		{Level(99), "unknown"},
		{Level(-1), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{" Debug ", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"err", LevelError},
		{"ERR", LevelError},
		{"unknown", LevelInfo},
		{"", LevelInfo},
	}
	for _, tt := range tests {
		if got := ParseLevel(tt.input); got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- Logger output tests ---

func TestLoggerBasicOutput(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Info("hello world", String("key", "value"), Int("n", 42))

	e := parseEntry(t, buf.Bytes())
	if e["level"] != "info" {
		t.Errorf("level = %v, want info", e["level"])
	}
	if e["msg"] != "hello world" {
		t.Errorf("msg = %v, want hello world", e["msg"])
	}
	if e["key"] != "value" {
		t.Errorf("key = %v, want value", e["key"])
	}
	if e["n"] != float64(42) {
		t.Errorf("n = %v, want 42", e["n"])
	}
	if _, ok := e["time"]; !ok {
		t.Error("missing time field")
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelWarn))

	l.Debug("debug msg")
	l.Info("info msg")
	l.Warn("warn msg")
	l.Error("error msg")

	entries := parseLines(t, buf.Bytes())
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0]["level"] != "warn" {
		t.Errorf("first entry level = %v, want warn", entries[0]["level"])
	}
	if entries[1]["level"] != "error" {
		t.Errorf("second entry level = %v, want error", entries[1]["level"])
	}
}

func TestLoggerDefaultLevelIsInfo(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf))

	l.Debug("should be filtered")
	l.Info("should appear")

	entries := parseLines(t, buf.Bytes())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0]["msg"] != "should appear" {
		t.Errorf("msg = %v, want 'should appear'", entries[0]["msg"])
	}
}

func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	child := l.With(String("component", "http"))
	child.Info("request", String("path", "/api/v1"))

	e := parseEntry(t, buf.Bytes())
	if e["component"] != "http" {
		t.Errorf("component = %v, want http", e["component"])
	}
	if e["path"] != "/api/v1" {
		t.Errorf("path = %v, want /api/v1", e["path"])
	}
}

func TestLoggerWithDoesNotMutateParent(t *testing.T) {
	var buf bytes.Buffer
	parent := New(WithWriter(&buf), WithLevel(LevelDebug))

	_ = parent.With(String("child", "yes"))
	parent.Info("from parent")

	e := parseEntry(t, buf.Bytes())
	if _, ok := e["child"]; ok {
		t.Error("parent should not have child's fields")
	}
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug), WithFields(String("app", "stanza")))

	l.Info("boot")

	e := parseEntry(t, buf.Bytes())
	if e["app"] != "stanza" {
		t.Errorf("app = %v, want stanza", e["app"])
	}
}

// --- Field type tests ---

func TestFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Info("types",
		String("s", "hello"),
		Int("i", 42),
		Int64("i64", 1234567890),
		Float64("f", 3.14),
		Bool("b", true),
		Duration("dur", 5*time.Second),
		Any("any", []int{1, 2, 3}),
	)

	e := parseEntry(t, buf.Bytes())
	if e["s"] != "hello" {
		t.Errorf("s = %v", e["s"])
	}
	if e["i"] != float64(42) {
		t.Errorf("i = %v", e["i"])
	}
	if e["i64"] != float64(1234567890) {
		t.Errorf("i64 = %v", e["i64"])
	}
	if e["f"] != 3.14 {
		t.Errorf("f = %v", e["f"])
	}
	if e["b"] != true {
		t.Errorf("b = %v", e["b"])
	}
	if e["dur"] != "5s" {
		t.Errorf("dur = %v", e["dur"])
	}
}

func TestFieldErr(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Error("failed", Err(errors.New("connection refused")))
	e := parseEntry(t, buf.Bytes())
	if e["error"] != "connection refused" {
		t.Errorf("error = %v, want connection refused", e["error"])
	}

	buf.Reset()
	l.Info("ok", Err(nil))
	e = parseEntry(t, buf.Bytes())
	if e["error"] != nil {
		t.Errorf("error = %v, want nil", e["error"])
	}
}

func TestFieldTime(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	l.Info("event", Time("ts", ts))

	e := parseEntry(t, buf.Bytes())
	if e["ts"] != "2024-06-15T10:30:00Z" {
		t.Errorf("ts = %v", e["ts"])
	}
}

// --- JSON escaping tests ---

func TestJSONEscaping(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"newline", "line1\nline2"},
		{"tab", "col1\tcol2"},
		{"quote", `say "hello"`},
		{"backslash", `path\to\file`},
		{"carriage return", "line1\rline2"},
		{"control char", "null\x00byte"},
		{"unicode", "hello \u4e16\u754c"},
		{"emoji", "fire \U0001f525"},
		{"mixed", "tab\there\nnewline\"quote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(WithWriter(&buf), WithLevel(LevelDebug))
			l.Info("test", String("data", tt.input))

			var e entry
			if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
				t.Fatalf("invalid JSON for input %q: %v\nraw: %s", tt.input, err, buf.String())
			}
			if e["data"] != tt.input {
				t.Errorf("data = %q, want %q", e["data"], tt.input)
			}
		})
	}
}

func TestJSONEscapingInMessage(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Info("msg with \"quotes\" and\nnewlines")
	e := parseEntry(t, buf.Bytes())
	if e["msg"] != "msg with \"quotes\" and\nnewlines" {
		t.Errorf("msg = %q", e["msg"])
	}
}

// --- Concurrency test ---

func TestLoggerConcurrency(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Info("concurrent", Int("n", n))
		}(i)
	}
	wg.Wait()

	entries := parseLines(t, buf.Bytes())
	if len(entries) != 100 {
		t.Errorf("expected 100 entries, got %d", len(entries))
	}
}

func TestLoggerWithConcurrency(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			child := l.With(Int("worker", n))
			child.Info("working")
		}(i)
	}
	wg.Wait()

	entries := parseLines(t, buf.Bytes())
	if len(entries) != 50 {
		t.Errorf("expected 50 entries, got %d", len(entries))
	}
}

// --- FileWriter tests ---

func TestFileWriterBasic(t *testing.T) {
	dir := t.TempDir()
	fw, err := NewFileWriter(dir)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	defer fw.Close()

	l := New(WithWriter(fw), WithLevel(LevelDebug))
	l.Info("test message", String("key", "value"))

	data, err := os.ReadFile(filepath.Join(dir, "stanza.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	e := parseEntry(t, data)
	if e["msg"] != "test message" {
		t.Errorf("msg = %v, want test message", e["msg"])
	}
}

func TestFileWriterRotationBySize(t *testing.T) {
	dir := t.TempDir()
	fw, err := NewFileWriter(dir, WithMaxSize(200), WithMaxFiles(10))
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	defer fw.Close()

	l := New(WithWriter(fw), WithLevel(LevelDebug))
	for i := 0; i < 20; i++ {
		l.Info("this is a longer message to trigger size-based rotation quickly", Int("i", i))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) < 2 {
		t.Errorf("expected at least 2 files after rotation, got %d", len(entries))
		for _, e := range entries {
			t.Logf("  %s", e.Name())
		}
	}

	// Verify current file exists and is valid JSON lines
	data, err := os.ReadFile(filepath.Join(dir, "stanza.log"))
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if len(data) == 0 {
		t.Error("current log file is empty")
	}
}

func TestFileWriterPrune(t *testing.T) {
	dir := t.TempDir()

	// Pre-create old rotated files
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "stanza-2024-01-0"+string(rune('1'+i))+".log")
		if err := os.WriteFile(name, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fw, err := NewFileWriter(dir, WithMaxSize(50), WithMaxFiles(2))
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	defer fw.Close()

	l := New(WithWriter(fw), WithLevel(LevelDebug))
	// Write enough to trigger rotation which triggers prune
	for i := 0; i < 10; i++ {
		l.Info("trigger rotation and pruning", Int("i", i))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Count rotated files (exclude stanza.log)
	rotatedCount := 0
	for _, e := range entries {
		if e.Name() != "stanza.log" {
			rotatedCount++
		}
	}

	if rotatedCount > 2 {
		t.Errorf("expected at most 2 rotated files, got %d", rotatedCount)
		for _, e := range entries {
			t.Logf("  %s", e.Name())
		}
	}
}

func TestFileWriterCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	fw, err := NewFileWriter(dir)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	defer fw.Close()

	l := New(WithWriter(fw), WithLevel(LevelDebug))
	l.Info("nested dir test")

	if _, err := os.Stat(filepath.Join(dir, "stanza.log")); err != nil {
		t.Errorf("log file not created in nested dir: %v", err)
	}
}

// --- All log levels test ---

func TestLoggerBoolFalse(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Info("test", Bool("flag", false))

	e := parseEntry(t, buf.Bytes())
	if e["flag"] != false {
		t.Errorf("flag = %v, want false", e["flag"])
	}
}

func TestLoggerNilValue(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Info("test", Any("val", nil))

	e := parseEntry(t, buf.Bytes())
	if e["val"] != nil {
		t.Errorf("val = %v, want nil", e["val"])
	}
}

func TestLoggerFloat64(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	l.Info("test", Float64("pi", 3.14159))

	e := parseEntry(t, buf.Bytes())
	if e["pi"] != 3.14159 {
		t.Errorf("pi = %v, want 3.14159", e["pi"])
	}
}

func TestInvalidUTF8Replacement(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	// \xff is invalid UTF-8 — should be replaced with U+FFFD.
	l.Info("test", String("data", "hello\xffworld"))

	var e entry
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	data := e["data"].(string)
	if !strings.Contains(data, "\ufffd") {
		t.Errorf("expected replacement character, got %q", data)
	}
}

func TestControlCharEscaping(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelDebug))

	// Characters below 0x20 (except \n, \r, \t) should be \u00xx escaped.
	l.Info("test", String("data", "bell\x07ack\x06"))

	var e entry
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if e["data"] != "bell\x07ack\x06" {
		t.Errorf("data = %q", e["data"])
	}
}

func TestFileWriterCloseNilFile(t *testing.T) {
	fw := &FileWriter{}
	if err := fw.Close(); err != nil {
		t.Errorf("close nil file: %v", err)
	}
}

func TestFileWriterMultipleSameDayRotations(t *testing.T) {
	dir := t.TempDir()
	fw, err := NewFileWriter(dir, WithMaxSize(50), WithMaxFiles(10))
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	defer fw.Close()

	l := New(WithWriter(fw), WithLevel(LevelDebug))
	// Write enough to trigger multiple rotations in the same day.
	for i := 0; i < 30; i++ {
		l.Info("trigger multiple same-day rotations with a longer message", Int("i", i))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should have stanza.log + at least 2 rotated files (with .1.log, .2.log suffixes).
	rotatedCount := 0
	for _, e := range entries {
		if e.Name() != "stanza.log" && strings.HasPrefix(e.Name(), "stanza-") {
			rotatedCount++
		}
	}
	if rotatedCount < 2 {
		t.Errorf("expected at least 2 rotated files for same-day rotation, got %d", rotatedCount)
	}
}

func TestLoggerChildInheritsLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithLevel(LevelWarn))

	child := l.With(String("component", "test"))
	child.Debug("should be filtered")
	child.Info("should be filtered")
	child.Warn("should appear")

	entries := parseLines(t, buf.Bytes())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0]["level"] != "warn" {
		t.Errorf("level = %v, want warn", entries[0]["level"])
	}
}

func TestAllLogLevels(t *testing.T) {
	tests := []struct {
		method string
		level  string
	}{
		{"Debug", "debug"},
		{"Info", "info"},
		{"Warn", "warn"},
		{"Error", "error"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(WithWriter(&buf), WithLevel(LevelDebug))

			switch tt.method {
			case "Debug":
				l.Debug("test")
			case "Info":
				l.Info("test")
			case "Warn":
				l.Warn("test")
			case "Error":
				l.Error("test")
			}

			e := parseEntry(t, buf.Bytes())
			if e["level"] != tt.level {
				t.Errorf("level = %v, want %v", e["level"], tt.level)
			}
		})
	}
}
