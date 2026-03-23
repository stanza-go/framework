package validate

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FieldError represents a single field validation failure. Use the
// validator functions (Required, MinLen, etc.) to create these.
type FieldError struct {
	Field   string
	Message string
}

// Validator holds collected field validation errors.
type Validator struct {
	errors map[string]string
	order  []string
}

// Fields creates a Validator from a list of field checks. Each check
// function returns nil if the field is valid, or a *FieldError if not.
// Only the first error per field is kept.
func Fields(checks ...*FieldError) *Validator {
	v := &Validator{errors: make(map[string]string)}
	for _, fe := range checks {
		if fe == nil {
			continue
		}
		if _, exists := v.errors[fe.Field]; !exists {
			v.errors[fe.Field] = fe.Message
			v.order = append(v.order, fe.Field)
		}
	}
	return v
}

// HasErrors returns true if any validation check failed.
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Errors returns the field errors as a map. The returned map should
// not be modified.
func (v *Validator) Errors() map[string]string {
	return v.errors
}

// Add appends additional field checks to an existing Validator. Only
// the first error per field is kept.
func (v *Validator) Add(checks ...*FieldError) {
	for _, fe := range checks {
		if fe == nil {
			continue
		}
		if _, exists := v.errors[fe.Field]; !exists {
			v.errors[fe.Field] = fe.Message
			v.order = append(v.order, fe.Field)
		}
	}
}

// WriteError writes a 422 JSON response with field-level errors.
// The response body is:
//
//	{"error": "validation failed", "fields": {"field": "message", ...}}
func (v *Validator) WriteError(w http.ResponseWriter) {
	resp := struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}{
		Error:  "validation failed",
		Fields: v.errors,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{\"error\":\"failed to encode validation errors\"}\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}

// --- Validator functions ---
// Each returns nil on success, or a *FieldError on failure.

// Required checks that a string value is non-empty after trimming
// whitespace.
func Required(field, value string) *FieldError {
	if strings.TrimSpace(value) == "" {
		return &FieldError{Field: field, Message: "is required"}
	}
	return nil
}

// MinLen checks that a string value has at least min characters.
// It does not check for empty strings — use Required for that.
func MinLen(field, value string, min int) *FieldError {
	if value != "" && len(value) < min {
		return &FieldError{Field: field, Message: "must be at least " + itoa(min) + " characters"}
	}
	return nil
}

// MaxLen checks that a string value has at most max characters.
func MaxLen(field, value string, max int) *FieldError {
	if len(value) > max {
		return &FieldError{Field: field, Message: "must be at most " + itoa(max) + " characters"}
	}
	return nil
}

// Email checks that a string value looks like a valid email address.
// It performs a basic structural check (contains @, has parts before
// and after @, domain has a dot) — not a full RFC 5322 parser.
func Email(field, value string) *FieldError {
	if value == "" {
		return nil
	}
	at := strings.LastIndex(value, "@")
	if at < 1 {
		return &FieldError{Field: field, Message: "must be a valid email address"}
	}
	domain := value[at+1:]
	if domain == "" || !strings.Contains(domain, ".") {
		return &FieldError{Field: field, Message: "must be a valid email address"}
	}
	dot := strings.LastIndex(domain, ".")
	if dot == len(domain)-1 {
		return &FieldError{Field: field, Message: "must be a valid email address"}
	}
	return nil
}

// URL checks that a string value is a valid HTTP or HTTPS URL. It verifies
// the scheme is http or https, the URL is parseable, and it has a host.
// An empty value is considered valid — use Required to enforce presence.
func URL(field, value string) *FieldError {
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return &FieldError{Field: field, Message: "must be a valid URL"}
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return &FieldError{Field: field, Message: "must be a valid URL"}
	}
	return nil
}

// PublicURL checks that a string value is a valid HTTP or HTTPS URL
// pointing to a public host. It rejects URLs with loopback addresses
// (127.x, ::1), private network addresses (10.x, 172.16-31.x,
// 192.168.x), link-local addresses (169.254.x, fe80::), and reserved
// hostnames (localhost, *.local). Use this for webhook URLs and other
// cases where the server will make outbound requests to user-supplied
// URLs, to prevent SSRF attacks.
// An empty value is considered valid — use Required to enforce presence.
func PublicURL(field, value string) *FieldError {
	if fe := URL(field, value); fe != nil {
		return fe
	}
	if value == "" {
		return nil
	}

	u, _ := url.Parse(value)
	host := u.Hostname()

	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return &FieldError{Field: field, Message: "must not point to a private or reserved address"}
		}
		return nil
	}

	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") ||
		strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".internal") {
		return &FieldError{Field: field, Message: "must not point to a private or reserved address"}
	}

	return nil
}

// OneOf checks that a string value is one of the allowed values.
// An empty value is considered valid — use Required to enforce
// presence.
func OneOf(field, value string, allowed ...string) *FieldError {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return &FieldError{Field: field, Message: "must be one of: " + strings.Join(allowed, ", ")}
}

// Positive checks that an integer value is greater than zero.
func Positive(field string, value int) *FieldError {
	if value <= 0 {
		return &FieldError{Field: field, Message: "must be a positive number"}
	}
	return nil
}

// InRange checks that an integer value is within [min, max] inclusive.
func InRange(field string, value, min, max int) *FieldError {
	if value < min || value > max {
		return &FieldError{Field: field, Message: "must be between " + itoa(min) + " and " + itoa(max)}
	}
	return nil
}

// FutureDate checks that a string value is a valid RFC 3339 timestamp
// in the future. An empty value is considered valid — use Required to
// enforce presence.
func FutureDate(field, value string) *FieldError {
	if value == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return &FieldError{Field: field, Message: "must be a valid ISO 8601 date"}
	}
	if !t.After(time.Now().UTC()) {
		return &FieldError{Field: field, Message: "must be a date in the future"}
	}
	return nil
}

// Slug checks that a string value is a valid URL slug: lowercase
// alphanumeric characters and hyphens, starting and ending with an
// alphanumeric character. An empty value is considered valid — use
// Required to enforce presence.
func Slug(field, value string) *FieldError {
	if value == "" {
		return nil
	}
	for i, c := range value {
		isAlnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		isHyphen := c == '-'
		if !isAlnum && !isHyphen {
			return &FieldError{Field: field, Message: "must contain only lowercase letters, numbers, and hyphens"}
		}
		if isHyphen && (i == 0 || i == len(value)-1) {
			return &FieldError{Field: field, Message: "must start and end with a letter or number"}
		}
	}
	return nil
}

// Check is a generic validator. If ok is false, it returns a
// FieldError with the given message. Use this for custom validation
// logic that doesn't fit the other validators.
func Check(field string, ok bool, message string) *FieldError {
	if !ok {
		return &FieldError{Field: field, Message: message}
	}
	return nil
}

// itoa converts an int to its string representation.
func itoa(n int) string {
	return strconv.Itoa(n)
}
