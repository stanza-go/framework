// Package config provides layered application configuration from defaults,
// YAML files, and environment variables. Values are resolved in priority order:
// environment variables > file values > defaults.
//
// Basic usage with defaults and environment variables:
//
//	cfg := config.New(config.WithDefaults(map[string]string{
//		"server.port": "8080",
//		"log.level":   "info",
//	}))
//	port := cfg.GetInt("server.port")
//
// Loading from a YAML file (missing file is silently skipped):
//
//	cfg, err := config.Load("/data/config.yaml", config.WithDefaults(map[string]string{
//		"server.port": "8080",
//	}))
//
// Environment variables override file and default values. A key "server.port"
// with prefix "STANZA" maps to the environment variable STANZA_SERVER_PORT.
//
// The YAML parser supports flat key-value pairs and one level of nesting:
//
//	server:
//	  port: 8080
//	  host: 0.0.0.0
//	log_level: debug
//
// Nested keys are flattened with dot notation: "server.port", "server.host".
package config
