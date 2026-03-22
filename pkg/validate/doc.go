// Package validate provides request input validation with field-level
// error collection. A Validator accumulates errors across multiple
// checks and reports them as a structured JSON response.
//
// Usage in an HTTP handler:
//
//	v := validate.Fields(
//	    validate.Required("email", req.Email),
//	    validate.Required("password", req.Password),
//	    validate.MinLen("password", req.Password, 8),
//	    validate.Email("email", req.Email),
//	    validate.OneOf("role", req.Role, "admin", "viewer", "superadmin"),
//	)
//	if v.HasErrors() {
//	    v.WriteError(w)
//	    return
//	}
//
// The error response is:
//
//	HTTP 422 Unprocessable Entity
//	{"error": "validation failed", "fields": {"email": "must be a valid email address", ...}}
//
// Only the first error per field is kept. Validators skip fields that
// already have an error, so ordering matters: put Required before
// MinLen for the same field.
package validate
