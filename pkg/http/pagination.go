package http

// Pagination holds parsed and validated pagination parameters extracted
// from an HTTP request's query string. Use ParsePagination to create one.
type Pagination struct {
	Limit  int
	Offset int
}

// ParsePagination extracts "limit" and "offset" query parameters from the
// request and validates them. The limit is clamped between 1 and maxLimit.
// The offset is clamped to non-negative.
//
// Example:
//
//	pg := http.ParsePagination(r, 50, 100) // default 50, max 100
//	selectQ.Limit(pg.Limit).Offset(pg.Offset)
func ParsePagination(r *Request, defaultLimit, maxLimit int) Pagination {
	limit := QueryParamInt(r, "limit", defaultLimit)
	offset := QueryParamInt(r, "offset", 0)

	if limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}

	return Pagination{Limit: limit, Offset: offset}
}

// PaginatedResponse writes a JSON response containing the paginated items
// and the total count. The items are written under the given key:
//
//	{"users": [...], "total": 42}
//
// Use this for endpoints that return a standard paginated list. For
// endpoints that need additional fields in the response (e.g., unread
// counts), use ParsePagination for input and write the response manually.
func PaginatedResponse(w ResponseWriter, key string, items any, total int) {
	WriteJSON(w, StatusOK, map[string]any{
		key:     items,
		"total": total,
	})
}
