// namespace_page.go renders the namespace management console page using server-side HTML templates.
package console

import (
	"html/template"
	"net/http"

	namespacesvc "etcdisc/internal/core/service/namespace"
)

var namespacePageTemplate = template.Must(template.New("namespaces").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>etcdisc Namespaces</title>
  <style>
    :root { color-scheme: light; --bg: #f5f0e8; --panel: #fffdf9; --ink: #182026; --accent: #b34d2e; --muted: #6a737d; }
    body { margin: 0; font-family: Georgia, "Times New Roman", serif; background: radial-gradient(circle at top left, #fff8ee, var(--bg)); color: var(--ink); }
    main { max-width: 960px; margin: 0 auto; padding: 32px 20px 48px; }
    h1 { margin: 0 0 12px; font-size: 2.2rem; }
    p { color: var(--muted); }
    .card { background: var(--panel); border: 1px solid rgba(24,32,38,0.08); border-radius: 16px; padding: 20px; box-shadow: 0 18px 40px rgba(24,32,38,0.08); }
    table { width: 100%; border-collapse: collapse; }
    th, td { text-align: left; padding: 12px 8px; border-bottom: 1px solid rgba(24,32,38,0.08); }
    th { font-size: 0.85rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--muted); }
    .badge { display: inline-block; padding: 4px 10px; border-radius: 999px; background: rgba(179,77,46,0.1); color: var(--accent); }
  </style>
</head>
<body>
  <main>
    <h1>Namespace Control</h1>
    <p>Current namespace: <strong>{{.Current}}</strong></p>
    <section class="card">
      <table>
        <thead><tr><th>Name</th><th>Access Mode</th><th>Revision</th></tr></thead>
        <tbody>
        {{range .Items}}
          <tr><td>{{.Name}}</td><td><span class="badge">{{.AccessMode}}</span></td><td>{{.Revision}}</td></tr>
        {{else}}
          <tr><td colspan="3">No namespaces yet.</td></tr>
        {{end}}
        </tbody>
      </table>
    </section>
  </main>
</body>
</html>`))

// NamespacePage renders the namespace console page.
type NamespacePage struct {
	Service *namespacesvc.Service
}

// ServeHTTP renders a namespace list filtered by the current namespace query parameter.
func (p NamespacePage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	items, err := p.Service.List(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	current := r.URL.Query().Get("namespace")
	if current == "" && len(items) > 0 {
		current = items[0].Name
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = namespacePageTemplate.Execute(w, map[string]any{"Items": items, "Current": current})
}
