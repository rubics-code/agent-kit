// Package web holds the built console (web/dist), for the Go binary to serve.
// Build it first with "npm run build" in web/.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built console. It has only dist/.keep before a build.
func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}

const notBuilt = `<!doctype html><meta charset="utf-8"><title>Agent Console</title>
<body style="font-family:system-ui;background:#0F1013;color:#ECEDEF;padding:40px">
<h1>The console is not built into this binary.</h1>
<p>Run <code>npm ci &amp;&amp; npm run build</code> in <code>web/</code>, then build the Go binary again.</p>`

// Handler serves the console from files. A path with no file gets index.html,
// so a reload on any page works. Hashed assets are cached for a year.
func Handler(files fs.FS) http.Handler {
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(files, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				static.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(p, "assets/") || path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(files, "index.html")
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		_, _ = w.Write(index)
	})
}
