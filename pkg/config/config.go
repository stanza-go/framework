package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds application configuration loaded from defaults, a YAML file,
// and environment variables. Values are resolved in priority order: environment
// variables > file values > defaults. Config is safe for concurrent reads
// after creation.
type Config struct {
	values   map[string]string
	defaults map[string]string
	prefix   string
	required []string
}

type options struct {
	defaults map[string]string
	prefix   string
	required []string
}

// Option configures a Config.
type Option func(*options)

// WithDefaults sets default values. These are used when neither an environment
// variable nor a file value is found for a key.
func WithDefaults(defaults map[string]string) Option {
	return func(o *options) {
		o.defaults = defaults
	}
}

// WithEnvPrefix sets the environment variable prefix. For a key "server.port"
// with prefix "STANZA", the environment variable STANZA_SERVER_PORT is checked.
// Default prefix is "STANZA".
func WithEnvPrefix(prefix string) Option {
	return func(o *options) {
		o.prefix = prefix
	}
}

// WithRequired marks keys that must have a non-empty value after loading.
// Validate returns an error listing all missing required keys.
func WithRequired(keys ...string) Option {
	return func(o *options) {
		o.required = append(o.required, keys...)
	}
}

// New creates a Config without loading a file. Configuration is read from
// defaults and environment variables only.
func New(opts ...Option) *Config {
	o := applyOptions(opts)
	return &Config{
		values:   make(map[string]string),
		defaults: o.defaults,
		prefix:   o.prefix,
		required: o.required,
	}
}

// Load creates a Config by reading a YAML file and applying the given options.
// If the file does not exist, it is silently skipped and the Config is created
// from defaults and environment variables only. Other read errors are returned.
func Load(path string, opts ...Option) (*Config, error) {
	o := applyOptions(opts)

	values := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: read file: %w", err)
	}
	if err == nil {
		parsed, parseErr := parseYAML(data)
		if parseErr != nil {
			return nil, parseErr
		}
		values = parsed
	}

	return &Config{
		values:   values,
		defaults: o.defaults,
		prefix:   o.prefix,
		required: o.required,
	}, nil
}

// Validate checks that all required keys have a non-empty value. It returns an
// error listing every missing key.
func (c *Config) Validate() error {
	var missing []string
	for _, key := range c.required {
		if c.resolve(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}
	return nil
}

// GetString returns the value for the given key, or an empty string if not
// found.
func (c *Config) GetString(key string) string {
	return c.resolve(key)
}

// GetStringOr returns the value for the given key, or fallback if the resolved
// value is empty.
func (c *Config) GetStringOr(key, fallback string) string {
	if v := c.resolve(key); v != "" {
		return v
	}
	return fallback
}

// GetInt returns the value for the given key as an int. Returns 0 if the key
// is missing or the value is not a valid integer.
func (c *Config) GetInt(key string) int {
	v, _ := strconv.Atoi(c.resolve(key))
	return v
}

// GetInt64 returns the value for the given key as an int64. Returns 0 if the
// key is missing or the value is not a valid integer.
func (c *Config) GetInt64(key string) int64 {
	v, _ := strconv.ParseInt(c.resolve(key), 10, 64)
	return v
}

// GetFloat64 returns the value for the given key as a float64. Returns 0 if
// the key is missing or the value is not a valid number.
func (c *Config) GetFloat64(key string) float64 {
	v, _ := strconv.ParseFloat(c.resolve(key), 64)
	return v
}

// GetBool returns the value for the given key as a bool. Returns false if the
// key is missing. Truthy values: "true", "1", "yes", "on" (case-insensitive).
func (c *Config) GetBool(key string) bool {
	switch strings.ToLower(c.resolve(key)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// GetDuration returns the value for the given key as a time.Duration. Returns
// 0 if the key is missing or the value is not a valid duration string.
func (c *Config) GetDuration(key string) time.Duration {
	v, _ := time.ParseDuration(c.resolve(key))
	return v
}

// Has returns true if the key has a non-empty value from any source.
func (c *Config) Has(key string) bool {
	return c.resolve(key) != ""
}

// resolve looks up a key in priority order: env > file > default.
func (c *Config) resolve(key string) string {
	if v := os.Getenv(c.envKey(key)); v != "" {
		return v
	}
	if v := c.values[key]; v != "" {
		return v
	}
	return c.defaults[key]
}

// envKey converts a config key to an environment variable name.
// "server.port" with prefix "STANZA" becomes "STANZA_SERVER_PORT".
func (c *Config) envKey(key string) string {
	k := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if c.prefix == "" {
		return k
	}
	return c.prefix + "_" + k
}

func applyOptions(opts []Option) options {
	o := options{
		prefix:   "STANZA",
		defaults: make(map[string]string),
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
