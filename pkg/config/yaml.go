package config

import (
	"bytes"
	"fmt"
	"strings"
)

// parseYAML parses a subset of YAML suitable for configuration files. It
// supports flat key-value pairs and one level of nesting. Nested keys are
// flattened with dot notation: a section "server" with key "port" becomes
// "server.port". Comments (#) and blank lines are ignored. All values are
// returned as strings.
//
// Supported syntax:
//
//	# comment
//	key: value
//	key: "quoted value"
//	key: 'single quoted'
//	section:
//	  nested_key: value
//	  another: 123
func parseYAML(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	lines := bytes.Split(data, []byte("\n"))

	var section string

	for i, line := range lines {
		lineNum := i + 1
		s := string(line)

		// Strip carriage return for Windows line endings.
		s = strings.TrimRight(s, "\r")

		// Skip blank lines and comments.
		trimmed := strings.TrimSpace(s)
		if trimmed == "" || trimmed[0] == '#' {
			continue
		}

		// Skip YAML document markers.
		if trimmed == "---" || trimmed == "..." {
			continue
		}

		// Determine indentation level.
		indent := len(s) - len(strings.TrimLeft(s, " \t"))

		// Find the colon separator (first colon).
		colonIdx := strings.IndexByte(trimmed, ':')
		if colonIdx < 0 {
			return nil, fmt.Errorf("config: yaml: line %d: expected key:value pair", lineNum)
		}

		key := strings.TrimSpace(trimmed[:colonIdx])
		val := strings.TrimSpace(trimmed[colonIdx+1:])

		if key == "" {
			return nil, fmt.Errorf("config: yaml: line %d: empty key", lineNum)
		}

		// Strip inline comments (outside quoted values).
		val = stripInlineComment(val)

		// Strip surrounding quotes.
		val = unquote(val)

		if indent == 0 {
			if val == "" {
				// Section header — following indented lines belong here.
				section = key
			} else {
				// Top-level scalar.
				section = ""
				result[key] = val
			}
		} else {
			if section == "" {
				return nil, fmt.Errorf("config: yaml: line %d: indented key without section", lineNum)
			}
			result[section+"."+key] = val
		}
	}

	return result, nil
}

// unquote removes matching surrounding quotes (single or double).
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// stripInlineComment removes a trailing " #" comment from an unquoted value.
func stripInlineComment(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		return s
	}
	if idx := strings.Index(s, " #"); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}
