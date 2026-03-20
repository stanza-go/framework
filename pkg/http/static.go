package http

import (
	"io/fs"
	nethttp "net/http"
	"path"
)

// Static returns a handler that serves static files from the given filesystem
// with SPA (Single Page Application) fallback. It reads the "path" wildcard
// parameter from the route pattern to determine which file to serve.
//
// When the requested path matches an existing file, that file is served.
// When no file matches and the path has no file extension, index.html is
// served instead — allowing client-side routing to handle the request.
// Requests for non-existent files with extensions (e.g., missing .js or .css)
// return 404.
//
// Usage:
//
//	router.Handle("GET /{path...}", http.Static(uiFS))
//	router.Handle("GET /admin/{path...}", http.Static(adminFS))
func Static(fsys fs.FS) Handler {
	return HandlerFunc(func(w ResponseWriter, r *Request) {
		p := r.PathValue("path")
		if p == "" {
			p = "index.html"
		}

		// Try to open the requested file.
		f, err := fsys.Open(p)
		if err == nil {
			stat, statErr := f.Stat()
			f.Close()
			if statErr == nil && !stat.IsDir() {
				nethttp.ServeFileFS(w, r, fsys, p)
				return
			}
		}

		// File not found. If the path has a file extension, it's a missing
		// static asset — return 404.
		if path.Ext(p) != "" {
			WriteError(w, StatusNotFound, "not found")
			return
		}

		// No extension — likely a client-side route. Serve index.html.
		nethttp.ServeFileFS(w, r, fsys, "index.html")
	})
}
