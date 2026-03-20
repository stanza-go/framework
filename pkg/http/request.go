package http

import (
	"encoding/json"
	"io"
	"strconv"
)

const maxBodySize int64 = 1 << 20 // 1 MB

// PathParam returns the value of the named path parameter from the
// request. Path parameters are defined in route patterns using
// {name} syntax:
//
//	r.HandleFunc("GET /users/{id}", func(w ResponseWriter, r *Request) {
//	    id := http.PathParam(r, "id")
//	})
func PathParam(r *Request, name string) string {
	return r.PathValue(name)
}

// QueryParam returns the value of the named query parameter. It
// returns an empty string if the parameter is not present.
func QueryParam(r *Request, name string) string {
	return r.URL.Query().Get(name)
}

// QueryParamOr returns the value of the named query parameter, or
// the fallback value if the parameter is missing or empty.
func QueryParamOr(r *Request, name, fallback string) string {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	return v
}

// QueryParamInt returns the integer value of the named query parameter.
// If the parameter is missing, empty, or not a valid integer, it
// returns the fallback value.
func QueryParamInt(r *Request, name string, fallback int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

// ReadJSON decodes the JSON request body into v. The body is limited
// to 1 MB. For a custom size limit, use ReadJSONLimit.
func ReadJSON(r *Request, v any) error {
	return ReadJSONLimit(r, v, maxBodySize)
}

// ReadJSONLimit decodes the JSON request body into v with a custom
// size limit in bytes.
func ReadJSONLimit(r *Request, v any, maxBytes int64) error {
	body := io.LimitReader(r.Body, maxBytes)
	return json.NewDecoder(body).Decode(v)
}
