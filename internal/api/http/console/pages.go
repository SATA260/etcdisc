// pages.go renders the remaining phase 1 admin console pages with lightweight server-side HTML.
package console

import (
	"context"
	"fmt"
	"html/template"
	"net/http"

	"etcdisc/internal/core/model"
	a2asvc "etcdisc/internal/core/service/a2a"
	auditsvc "etcdisc/internal/core/service/audit"
	configsvc "etcdisc/internal/core/service/config"
	registrysvc "etcdisc/internal/core/service/registry"
)

var pageTemplate = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root { --bg: #f7f3ed; --panel: #fffdf9; --ink: #1a222b; --muted: #6b7280; --accent: #b65a31; }
    body { margin: 0; background: linear-gradient(180deg, #fff7ea, var(--bg)); color: var(--ink); font-family: Georgia, "Times New Roman", serif; }
    main { max-width: 1040px; margin: 0 auto; padding: 32px 20px 56px; }
    h1 { margin: 0 0 10px; font-size: 2.2rem; }
    p { color: var(--muted); }
    .card { background: var(--panel); border: 1px solid rgba(26,34,43,0.08); border-radius: 18px; padding: 22px; box-shadow: 0 20px 40px rgba(26,34,43,0.08); }
    table { width: 100%; border-collapse: collapse; }
    th, td { text-align: left; padding: 12px 8px; border-bottom: 1px solid rgba(26,34,43,0.08); vertical-align: top; }
    th { font-size: 0.84rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--muted); }
    .tag { display: inline-block; padding: 4px 10px; border-radius: 999px; background: rgba(182,90,49,0.12); color: var(--accent); margin-right: 6px; }
    .mono { font-family: "Courier New", monospace; font-size: 0.92rem; }
  </style>
</head>
<body>
  <main>
    <h1>{{.Title}}</h1>
    <p>{{.Subtitle}}</p>
    <section class="card">{{template "content" .}}</section>
  </main>
</body>
</html>`))

// ServiceOverviewPage renders services and runtime instances.
type ServiceOverviewPage struct {
	Registry *registrysvc.Service
}

// ServeHTTP renders the service overview page.
func (p ServiceOverviewPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespaceName := queryOrDefault(r, "namespace", "default")
	items, err := p.Registry.List(r.Context(), registrysvc.ListFilter{Namespace: namespaceName, Service: r.URL.Query().Get("service"), HealthyOnly: false, Admin: true})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	tmpl := template.Must(pageTemplate.Clone())
	template.Must(tmpl.New("content").Parse(`<table><thead><tr><th>Service</th><th>Instance</th><th>Status</th><th>Endpoint</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Service}}</td><td>{{.InstanceID}}</td><td><span class="tag">{{.Status}}</span></td><td class="mono">{{.Address}}:{{.Port}}</td></tr>{{else}}<tr><td colspan="4">No instances found.</td></tr>{{end}}</tbody></table>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]any{"Title": "Service Overview", "Subtitle": fmt.Sprintf("Namespace %s runtime inventory.", namespaceName), "Rows": items})
}

// PolicyPage renders raw service and namespace config items.
type PolicyPage struct {
	Config *configsvc.Service
}

// ServeHTTP renders the policy configuration page.
func (p PolicyPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespaceName := queryOrDefault(r, "namespace", "default")
	serviceName := r.URL.Query().Get("service")
	items, err := p.Config.GetRaw(r.Context(), configScopeOrDefault(r), namespaceName, serviceName, true)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	tmpl := template.Must(pageTemplate.Clone())
	template.Must(tmpl.New("content").Parse(`<table><thead><tr><th>Scope</th><th>Key</th><th>Value</th><th>Type</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Scope}}</td><td class="mono">{{.Key}}</td><td class="mono">{{.Value}}</td><td>{{.ValueType}}</td></tr>{{else}}<tr><td colspan="4">No config items found.</td></tr>{{end}}</tbody></table>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]any{"Title": "Policy Config", "Subtitle": fmt.Sprintf("Namespace %s service %s policy view.", namespaceName, serviceName), "Rows": items})
}

// A2APage renders AgentCards and capability information.
type A2APage struct {
	A2A *a2asvc.Service
}

// ServeHTTP renders the A2A overview page.
func (p A2APage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespaceName := queryOrDefault(r, "namespace", "default")
	items, err := p.A2A.List(r.Context(), namespaceName, true)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	tmpl := template.Must(pageTemplate.Clone())
	template.Must(tmpl.New("content").Parse(`<table><thead><tr><th>Agent</th><th>Service</th><th>Capabilities</th><th>Protocols</th><th>Auth</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.AgentID}}</td><td>{{.Service}}</td><td>{{range .Capabilities}}<span class="tag">{{.}}</span>{{end}}</td><td>{{range .Protocols}}<span class="tag">{{.}}</span>{{end}}</td><td>{{.AuthMode}}</td></tr>{{else}}<tr><td colspan="5">No AgentCards found.</td></tr>{{end}}</tbody></table>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]any{"Title": "A2A AgentCards", "Subtitle": fmt.Sprintf("Namespace %s capability inventory.", namespaceName), "Rows": items})
}

// AuditPage renders audit log entries.
type AuditPage struct {
	Audit *auditsvc.Service
}

// ServeHTTP renders the audit log page.
func (p AuditPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	items, err := p.Audit.List(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	tmpl := template.Must(pageTemplate.Clone())
	template.Must(tmpl.New("content").Parse(`<table><thead><tr><th>Time</th><th>Action</th><th>Resource</th><th>Message</th></tr></thead><tbody>{{range .Rows}}<tr><td class="mono">{{.CreatedAt}}</td><td>{{.Action}}</td><td>{{.Resource}}</td><td>{{.Message}}</td></tr>{{else}}<tr><td colspan="4">No audit entries found.</td></tr>{{end}}</tbody></table>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]any{"Title": "Audit Log", "Subtitle": "Recent admin and system events.", "Rows": items})
}

// SystemPage renders ready and metrics metadata.
type SystemPage struct {
	Ready func(context.Context) error
}

// ServeHTTP renders the system status page.
func (p SystemPage) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	ready := true
	if p.Ready != nil {
		ready = p.Ready(context.Background()) == nil
	}
	tmpl := template.Must(pageTemplate.Clone())
	template.Must(tmpl.New("content").Parse(`<table><thead><tr><th>Check</th><th>Value</th></tr></thead><tbody><tr><td>Ready</td><td><span class="tag">{{.Ready}}</span></td></tr><tr><td>Metrics</td><td class="mono">/metrics</td></tr><tr><td>Health</td><td class="mono">/healthz</td></tr><tr><td>Readiness</td><td class="mono">/ready</td></tr></tbody></table>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]any{"Title": "System Status", "Subtitle": "Operational endpoints and current ready state.", "Ready": ready})
}

func queryOrDefault(r *http.Request, key, fallback string) string {
	if value := r.URL.Query().Get(key); value != "" {
		return value
	}
	return fallback
}

func configScopeOrDefault(r *http.Request) model.ConfigScope {
	scope := model.ConfigScope(r.URL.Query().Get("scope"))
	if scope.Valid() {
		return scope
	}
	return model.ConfigScopeService
}
