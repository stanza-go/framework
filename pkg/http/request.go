package http

import (
	"encoding/json"
	"errors"
	"io"
	nethttp "net/http"
	"strconv"
	"strings"
)

// ErrBodyTooLarge is returned by ReadJSON and ReadJSONLimit when the
// request body exceeds the configured size limit.
var ErrBodyTooLarge = errors.New("http: request body too large")

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

// QueryParamSort reads "sort" and "order" query parameters and validates
// them against the allowed columns. Returns the validated column and
// direction ("ASC" or "DESC"). If the sort parameter is missing or not
// in the allowed list, the defaults are returned.
//
// Example:
//
//	col, dir := http.QueryParamSort(r, []string{"id", "email", "name", "created_at"}, "id", "DESC")
//	selectQ.OrderBy(col, dir)
func QueryParamSort(r *Request, allowed []string, defaultCol, defaultDir string) (string, string) {
	col := strings.ToLower(r.URL.Query().Get("sort"))
	dir := strings.ToUpper(r.URL.Query().Get("order"))

	validCol := false
	for _, a := range allowed {
		if col == a {
			validCol = true
			break
		}
	}
	if !validCol {
		col = defaultCol
	}

	if dir != "ASC" && dir != "DESC" {
		dir = defaultDir
	}

	return col, dir
}

// ReadJSON decodes the JSON request body into v. The body is limited
// to 1 MB. For a custom size limit, use ReadJSONLimit.
func ReadJSON(r *Request, v any) error {
	return ReadJSONLimit(r, v, maxBodySize)
}

// ReadJSONLimit decodes the JSON request body into v with a custom
// size limit in bytes. If the body exceeds the limit, ErrBodyTooLarge
// is returned.
func ReadJSONLimit(r *Request, v any, maxBytes int64) error {
	lr := &bodyLimitReader{r: r.Body, n: maxBytes}
	err := json.NewDecoder(lr).Decode(v)
	if err != nil {
		// MaxBody middleware wraps the body with MaxBytesReader. If
		// the middleware's limit is hit before ours, detect it.
		var maxErr *nethttp.MaxBytesError
		if errors.As(err, &maxErr) {
			return ErrBodyTooLarge
		}
		if errors.Is(err, ErrBodyTooLarge) {
			return ErrBodyTooLarge
		}
		return err
	}
	return nil
}

// BindJSON reads the JSON request body into v. If the body is missing,
// malformed, or exceeds 1 MB, it writes a 400 error response and returns
// false. The caller should return immediately when false:
//
//	var req createRequest
//	if !http.BindJSON(w, r, &req) {
//	    return
//	}
func BindJSON(w ResponseWriter, r *Request, v any) bool {
	if err := ReadJSON(r, v); err != nil {
		WriteError(w, StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// PathParamInt64 parses a path parameter as an int64. If the value is
// missing or not a valid integer, it writes a 400 error response and
// returns false. The caller should return immediately when false:
//
//	id, ok := http.PathParamInt64(w, r, "id")
//	if !ok {
//	    return
//	}
func PathParamInt64(w ResponseWriter, r *Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		WriteError(w, StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return v, true
}

// CheckBulkIDs validates that an int64 ID slice is non-empty and does
// not exceed maxCount. If invalid, it writes a 400 error response and
// returns false. The caller should return immediately when false:
//
//	if !http.CheckBulkIDs(w, req.IDs, 100) {
//	    return
//	}
func CheckBulkIDs(w ResponseWriter, ids []int64, maxCount int) bool {
	if len(ids) == 0 {
		WriteError(w, StatusBadRequest, "ids required")
		return false
	}
	if len(ids) > maxCount {
		WriteError(w, StatusBadRequest, "maximum "+strconv.Itoa(maxCount)+" ids per request")
		return false
	}
	return true
}

// bodyLimitReader wraps an io.Reader with a byte limit. Unlike
// io.LimitReader, it returns ErrBodyTooLarge instead of io.EOF when
// the limit is reached, giving callers a clear signal that the body
// was too large rather than simply exhausted.
type bodyLimitReader struct {
	r io.Reader
	n int64
}

func (lr *bodyLimitReader) Read(p []byte) (int, error) {
	if lr.n <= 0 {
		return 0, ErrBodyTooLarge
	}
	if int64(len(p)) > lr.n {
		p = p[:lr.n]
	}
	n, err := lr.r.Read(p)
	lr.n -= int64(n)
	return n, err
}
