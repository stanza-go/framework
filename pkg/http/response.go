package http

import (
	"encoding/csv"
	"encoding/json"
	"time"
)

// Common HTTP status codes re-exported for convenience, so consumers
// do not need a separate net/http import for status codes.
const (
	StatusOK                  = 200
	StatusCreated             = 201
	StatusNoContent           = 204
	StatusNotModified         = 304
	StatusBadRequest          = 400
	StatusUnauthorized        = 401
	StatusForbidden           = 403
	StatusNotFound            = 404
	StatusMethodNotAllowed    = 405
	StatusConflict            = 409
	StatusRequestEntityTooLarge = 413
	StatusUnprocessableEntity   = 422
	StatusTooManyRequests       = 429
	StatusInternalServerError = 500
	StatusServiceUnavailable  = 503
)

// WriteJSON writes v as a JSON response with the given HTTP status code.
// It sets the Content-Type header to application/json. If JSON encoding
// fails, it writes a 500 error response instead.
func WriteJSON(w ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(StatusInternalServerError)
		_, _ = w.Write([]byte("{\"error\":\"failed to encode response\"}\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}

// WriteError writes a JSON error response with the given HTTP status
// code and error message. The response body is:
//
//	{"error": "message"}
func WriteError(w ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// WriteCSV writes a CSV file response. It sets Content-Type and
// Content-Disposition headers, writes the header row, then calls fn
// repeatedly to produce data rows. The fn callback should return the
// next row as a string slice, or nil to stop iteration.
//
// Example:
//
//	rows, _ := db.Query(sql, args...)
//	defer rows.Close()
//	http.WriteCSV(w, "users", []string{"ID", "Email", "Name"}, func() []string {
//	    if !rows.Next() {
//	        return nil
//	    }
//	    var id int64
//	    var email, name string
//	    if err := rows.Scan(&id, &email, &name); err != nil {
//	        return nil
//	    }
//	    return []string{strconv.FormatInt(id, 10), email, name}
//	})
func WriteCSV(w ResponseWriter, entity string, header []string, fn func() []string) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+entity+"-"+time.Now().UTC().Format("20060102")+".csv")
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for {
		row := fn()
		if row == nil {
			break
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}
