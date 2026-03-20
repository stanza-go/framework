package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- New and defaults ---

func TestNewEmpty(t *testing.T) {
	cfg := New()
	if cfg.GetString("nonexistent") != "" {
		t.Fatal("expected empty string for missing key")
	}
}

func TestDefaults(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{
		"server.port": "8080",
		"log.level":   "info",
	}))
	if v := cfg.GetString("server.port"); v != "8080" {
		t.Fatalf("GetString = %q, want 8080", v)
	}
	if v := cfg.GetString("log.level"); v != "info" {
		t.Fatalf("GetString = %q, want info", v)
	}
}

// --- Environment variable override ---

func TestEnvOverridesDefault(t *testing.T) {
	t.Setenv("STANZA_SERVER_PORT", "9090")
	cfg := New(WithDefaults(map[string]string{
		"server.port": "8080",
	}))
	if v := cfg.GetString("server.port"); v != "9090" {
		t.Fatalf("expected env override, got %q", v)
	}
}

func TestCustomEnvPrefix(t *testing.T) {
	t.Setenv("MYAPP_SERVER_PORT", "7777")
	cfg := New(WithEnvPrefix("MYAPP"))
	if v := cfg.GetString("server.port"); v != "7777" {
		t.Fatalf("expected custom prefix, got %q", v)
	}
}

func TestEmptyEnvPrefix(t *testing.T) {
	t.Setenv("SERVER_PORT", "6666")
	cfg := New(WithEnvPrefix(""))
	if v := cfg.GetString("server.port"); v != "6666" {
		t.Fatalf("expected empty prefix, got %q", v)
	}
}

// --- Load from YAML file ---

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, `
# Server settings
server:
  port: 3000
  host: localhost

log:
  level: debug
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if v := cfg.GetString("server.port"); v != "3000" {
		t.Fatalf("server.port = %q, want 3000", v)
	}
	if v := cfg.GetString("server.host"); v != "localhost" {
		t.Fatalf("server.host = %q, want localhost", v)
	}
	if v := cfg.GetString("log.level"); v != "debug" {
		t.Fatalf("log.level = %q, want debug", v)
	}
}

func TestLoadMissingFileSkipped(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected missing file to be skipped, got: %v", err)
	}
	if cfg.GetString("key") != "" {
		t.Fatal("expected empty config from missing file")
	}
}

// --- Priority chain: env > file > default ---

func TestPriorityChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "server:\n  port: 3000\n")

	t.Setenv("STANZA_SERVER_PORT", "9090")

	cfg, err := Load(path, WithDefaults(map[string]string{
		"server.port": "8080",
	}))
	if err != nil {
		t.Fatal(err)
	}

	// Env wins over file and default.
	if v := cfg.GetString("server.port"); v != "9090" {
		t.Fatalf("expected env to win, got %q", v)
	}
}

func TestFileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "server:\n  port: 3000\n")

	cfg, err := Load(path, WithDefaults(map[string]string{
		"server.port": "8080",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if v := cfg.GetString("server.port"); v != "3000" {
		t.Fatalf("expected file to override default, got %q", v)
	}
}

// --- Typed getters ---

func TestGetInt(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"port": "8080"}))
	if v := cfg.GetInt("port"); v != 8080 {
		t.Fatalf("GetInt = %d, want 8080", v)
	}
}

func TestGetIntInvalid(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"port": "abc"}))
	if v := cfg.GetInt("port"); v != 0 {
		t.Fatalf("GetInt invalid = %d, want 0", v)
	}
}

func TestGetIntMissing(t *testing.T) {
	cfg := New()
	if v := cfg.GetInt("missing"); v != 0 {
		t.Fatalf("GetInt missing = %d, want 0", v)
	}
}

func TestGetInt64(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"big": "1234567890123"}))
	if v := cfg.GetInt64("big"); v != 1234567890123 {
		t.Fatalf("GetInt64 = %d, want 1234567890123", v)
	}
}

func TestGetFloat64(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"rate": "3.14"}))
	if v := cfg.GetFloat64("rate"); v != 3.14 {
		t.Fatalf("GetFloat64 = %f, want 3.14", v)
	}
}

func TestGetDuration(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"timeout": "5s"}))
	if v := cfg.GetDuration("timeout"); v != 5*time.Second {
		t.Fatalf("GetDuration = %v, want 5s", v)
	}
}

func TestGetDurationInvalid(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"timeout": "not-a-duration"}))
	if v := cfg.GetDuration("timeout"); v != 0 {
		t.Fatalf("GetDuration invalid = %v, want 0", v)
	}
}

func TestGetBool(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"Yes", true},
		{"on", true},
		{"ON", true},
		{"false", false},
		{"False", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"maybe", false},
	}
	for _, tt := range tests {
		cfg := New(WithDefaults(map[string]string{"k": tt.val}))
		if got := cfg.GetBool("k"); got != tt.want {
			t.Errorf("GetBool(%q) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestGetBoolMissing(t *testing.T) {
	cfg := New()
	if cfg.GetBool("missing") {
		t.Fatal("expected false for missing key")
	}
}

// --- GetStringOr ---

func TestGetStringOr(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"exists": "value"}))
	if v := cfg.GetStringOr("exists", "fallback"); v != "value" {
		t.Fatalf("expected value, got %q", v)
	}
	if v := cfg.GetStringOr("missing", "fallback"); v != "fallback" {
		t.Fatalf("expected fallback, got %q", v)
	}
}

// --- Has ---

func TestHas(t *testing.T) {
	cfg := New(WithDefaults(map[string]string{"exists": "value"}))
	if !cfg.Has("exists") {
		t.Fatal("expected Has to return true for existing key")
	}
	if cfg.Has("missing") {
		t.Fatal("expected Has to return false for missing key")
	}
}

// --- Validate ---

func TestValidatePass(t *testing.T) {
	cfg := New(
		WithDefaults(map[string]string{
			"db.path":     "/data/db.sqlite",
			"server.port": "8080",
		}),
		WithRequired("db.path", "server.port"),
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFail(t *testing.T) {
	cfg := New(WithRequired("db.path", "server.port"))
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if got := err.Error(); got != "config: missing required keys: db.path, server.port" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestValidatePartial(t *testing.T) {
	cfg := New(
		WithDefaults(map[string]string{"db.path": "/data/db.sqlite"}),
		WithRequired("db.path", "server.port"),
	)
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if got := err.Error(); got != "config: missing required keys: server.port" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestValidateFromEnv(t *testing.T) {
	t.Setenv("STANZA_DB_PATH", "/data/db.sqlite")
	cfg := New(WithRequired("db.path"))
	if err := cfg.Validate(); err != nil {
		t.Fatalf("env should satisfy required: %v", err)
	}
}

// --- envKey ---

func TestEnvKeyMapping(t *testing.T) {
	tests := []struct {
		prefix string
		key    string
		want   string
	}{
		{"STANZA", "server.port", "STANZA_SERVER_PORT"},
		{"STANZA", "log.level", "STANZA_LOG_LEVEL"},
		{"STANZA", "data_dir", "STANZA_DATA_DIR"},
		{"MYAPP", "db.path", "MYAPP_DB_PATH"},
		{"", "server.port", "SERVER_PORT"},
	}
	for _, tt := range tests {
		cfg := &Config{prefix: tt.prefix}
		if got := cfg.envKey(tt.key); got != tt.want {
			t.Errorf("envKey(%q) with prefix %q = %q, want %q", tt.key, tt.prefix, got, tt.want)
		}
	}
}

// --- YAML parser ---

func TestParseYAMLFlat(t *testing.T) {
	input := `# A comment
name: myapp
debug: true
port: 8080`

	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "name", "myapp")
	assertMapVal(t, m, "debug", "true")
	assertMapVal(t, m, "port", "8080")
}

func TestParseYAMLNested(t *testing.T) {
	input := `
server:
  port: 3000
  host: 0.0.0.0
database:
  path: /data/db.sqlite
`

	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "server.port", "3000")
	assertMapVal(t, m, "server.host", "0.0.0.0")
	assertMapVal(t, m, "database.path", "/data/db.sqlite")
}

func TestParseYAMLMixed(t *testing.T) {
	input := `
app_name: stanza
server:
  port: 8080
debug: true
`

	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "app_name", "stanza")
	assertMapVal(t, m, "server.port", "8080")
	assertMapVal(t, m, "debug", "true")
}

func TestParseYAMLQuotedValues(t *testing.T) {
	input := `name: "my app"
path: '/usr/local/bin'
plain: no quotes`

	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "name", "my app")
	assertMapVal(t, m, "path", "/usr/local/bin")
	assertMapVal(t, m, "plain", "no quotes")
}

func TestParseYAMLInlineComment(t *testing.T) {
	input := `port: 8080 # the port
name: "val # not a comment"
`

	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "port", "8080")
	assertMapVal(t, m, "name", "val # not a comment")
}

func TestParseYAMLWindowsLineEndings(t *testing.T) {
	input := "key1: value1\r\nkey2: value2\r\n"
	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "key1", "value1")
	assertMapVal(t, m, "key2", "value2")
}

func TestParseYAMLDocumentMarkers(t *testing.T) {
	input := "---\nkey: value\n..."
	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "key", "value")
}

func TestParseYAMLEmpty(t *testing.T) {
	m, err := parseYAML([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}
}

func TestParseYAMLOnlyComments(t *testing.T) {
	input := "# comment 1\n# comment 2\n"
	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}
}

func TestParseYAMLValueWithColon(t *testing.T) {
	input := `url: http://localhost:8080`
	m, err := parseYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	assertMapVal(t, m, "url", "http://localhost:8080")
}

func TestParseYAMLErrorNoColon(t *testing.T) {
	_, err := parseYAML([]byte("not a valid yaml line"))
	if err == nil {
		t.Fatal("expected error for line without colon")
	}
}

func TestParseYAMLErrorOrphanedIndent(t *testing.T) {
	_, err := parseYAML([]byte("  orphan: value"))
	if err == nil {
		t.Fatal("expected error for indented key without section")
	}
}

func TestParseYAMLErrorEmptyKey(t *testing.T) {
	_, err := parseYAML([]byte(": value"))
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertMapVal(t *testing.T, m map[string]string, key, want string) {
	t.Helper()
	if got := m[key]; got != want {
		t.Errorf("m[%q] = %q, want %q", key, got, want)
	}
}
