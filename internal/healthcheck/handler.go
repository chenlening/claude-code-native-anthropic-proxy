package healthcheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/anthropic-transparent-proxy/internal/config"
	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/metrics"
)

// HealthResponse is the JSON response for /health endpoint
type HealthResponse struct {
	Status        string                              `json:"status"`
	TotalRequests int64                               `json:"total_requests"`
	Endpoints     map[string]metrics.EndpointSnapshot `json:"endpoints"`
	Models        map[string]metrics.ModelSnapshot    `json:"models"`
	ByBackend     []metrics.BackendStats              `json:"by_backend"`
}

// Handler handles /health requests
type Handler struct {
	healthMgr *endpoint.HealthManager
	metrics   *metrics.Metrics
	cfg       *config.Config
}

// NewHandler creates a new health handler
func NewHandler(hm *endpoint.HealthManager, m *metrics.Metrics, cfg *config.Config) *Handler {
	return &Handler{healthMgr: hm, metrics: m, cfg: cfg}
}

// ServeHTTP handles the health check request
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := h.buildHealthData()

	// Set HTTP status BEFORE writing body (WriteHeader after Write is ignored)
	if data.Status == "unhealthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// Serve HTML if browser requests it, otherwise JSON
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		h.renderHTML(w, data)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

func (h *Handler) buildHealthData() HealthResponse {
	endpoints := h.healthMgr.GetHealthyEndpoints()
	disabled := h.healthMgr.GetDisabledEndpoints()

	resp := HealthResponse{
		Endpoints: make(map[string]metrics.EndpointSnapshot),
		Models:    make(map[string]metrics.ModelSnapshot),
	}

	snap := h.metrics.Snapshot()
	resp.TotalRequests = snap.TotalRequests
	resp.ByBackend = snap.ByBackend

	for epName, epSnap := range snap.Endpoints {
		resp.Endpoints[epName] = epSnap
	}
	for model, modelSnap := range snap.Models {
		resp.Models[model] = modelSnap
	}

	for _, ep := range endpoints {
		eps := resp.Endpoints[ep.Name]
		eps.Status = "enabled"
		eps.ActiveConnections = int(ep.GetTotalConnections())
		if t := ep.GetLastRequestTime(); !t.IsZero() {
			eps.LastRequestTime = &t
		}
		if t := ep.GetLastFailureTime(); !t.IsZero() {
			eps.LastFailureTime = &t
		}
		eps.LastFailureReason = ep.GetLastFailureReason()
		eps.LastDiscoveryError = ep.GetDiscoveryError()
		eps.SupportedModels = ep.GetSupportedModels()
		resp.Endpoints[ep.Name] = eps
	}
	for _, ep := range disabled {
		eps := resp.Endpoints[ep.Name]
		eps.Status = "disabled"
		eps.ActiveConnections = int(ep.GetTotalConnections())
		if t := ep.GetLastRequestTime(); !t.IsZero() {
			eps.LastRequestTime = &t
		}
		if t := ep.GetLastFailureTime(); !t.IsZero() {
			eps.LastFailureTime = &t
		}
		eps.LastFailureReason = ep.GetLastFailureReason()
		eps.LastDiscoveryError = ep.GetDiscoveryError()
		if t := ep.GetLastProbeTime(); !t.IsZero() {
			eps.LastProbeTime = &t
			probeSuccess := ep.GetLastProbeSuccess()
			eps.LastProbeSuccess = &probeSuccess
		}
		eps.SupportedModels = ep.GetSupportedModels()
		resp.Endpoints[ep.Name] = eps
	}

	// Add offline endpoints from config (intentionally excluded from routing)
	if h.cfg != nil {
		for name, epCfg := range h.cfg.Endpoints {
			if epCfg.Offline {
				resp.Endpoints[name] = metrics.EndpointSnapshot{
					Status: "offline",
				}
			}
		}
	}

	totalEndpoints := len(endpoints) + len(disabled)
	if totalEndpoints == 0 {
		resp.Status = "unhealthy"
	} else if len(endpoints) == 0 {
		resp.Status = "unhealthy"
	} else if len(disabled) == 0 {
		resp.Status = "healthy"
	} else {
		resp.Status = "degraded"
	}

	return resp
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Format("15:04:05")
}

func truncateFailureReason(reason string, maxLen int) string {
	if reason == "" {
		return "—"
	}
	if len(reason) <= maxLen {
		return reason
	}
	return reason[:maxLen] + "..."
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (h *Handler) renderHTML(w http.ResponseWriter, data HealthResponse) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	statusColor := "#22c55e" // green
	if data.Status == "degraded" {
		statusColor = "#f59e0b" // amber
	} else if data.Status == "unhealthy" {
		statusColor = "#ef4444" // red
	}

	var sb strings.Builder

	// Header with auto-refresh
	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Proxy Health</title>
<meta http-equiv="refresh" content="10">
<style>
*{box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;margin:0;padding:20px}
.container{max-width:1200px;margin:0 auto}
h1{margin:0 0 4px;font-size:22px;text-align:center}
h2{margin:0 0 10px;font-size:14px;color:#94a3b8;text-transform:uppercase;letter-spacing:0.5px}
.status-line{text-align:center;margin-bottom:20px}
.status-dot{font-size:18px;vertical-align:middle}
.timestamp{color:#64748b;font-size:12px;text-align:center;margin-bottom:24px}
.card{background:#1e293b;border-radius:8px;padding:16px;margin-bottom:14px}
.stat{display:flex;justify-content:center;gap:32px;flex-wrap:wrap;margin-bottom:24px}
.stat-item{text-align:center}
.stat-value{font-size:28px;font-weight:700}
.stat-label{font-size:11px;color:#64748b;text-transform:uppercase;letter-spacing:0.5px}
.table-wrap{margin:0 -4px}
table{width:100%;border-collapse:collapse}
th,td{padding:6px 8px;border-bottom:1px solid #334155;white-space:nowrap}
th{white-space:normal}
th{color:#94a3b8;font-size:11px;text-transform:uppercase;text-align:left}
td{font-size:13px}
td code{background:#334155;padding:2px 6px;border-radius:4px;font-size:12px}
.badge{display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;color:#fff}
.green{background:#22c55e}
.amber{background:#f59e0b}
.red{background:#ef4444}
.gray{background:#64748b}
.footer{text-align:center;padding-top:8px}
.footer a{color:#60a5fa;text-decoration:none;font-size:12px}
.footer span{color:#475569;font-size:12px}
.models{display:flex;flex-wrap:wrap;gap:4px}
.model-tag{background:#334155;padding:2px 6px;border-radius:4px;font-size:11px}
.model-header{background:#334155;padding:8px;margin-bottom:8px;border-radius:4px;font-size:13px;color:#e2e8f0}
</style>
</head>
<body>
<div class="container">
`)

	// Status header
	sb.WriteString(fmt.Sprintf(`<h1><span class="status-dot" style="color:%s">●</span> Proxy %s</h1>
`, statusColor, strings.ToUpper(data.Status[:1])+data.Status[1:]))
	sb.WriteString(fmt.Sprintf(`<p class="timestamp">Last updated: %s | Auto-refreshes every 10s</p>
`, time.Now().Format("2006-01-02 15:04:05")))

	// Summary stats
	sb.WriteString(`<div class="stat">
`)
	sb.WriteString(fmt.Sprintf(`<div class="stat-item"><div class="stat-value">%d</div><div class="stat-label">Total Requests</div></div>
`, data.TotalRequests))
	sb.WriteString(fmt.Sprintf(`<div class="stat-item"><div class="stat-value">%d</div><div class="stat-label">Endpoints</div></div>
`, len(data.Endpoints)))
	sb.WriteString(fmt.Sprintf(`<div class="stat-item"><div class="stat-value">%d</div><div class="stat-label">Backend Models</div></div>
`, len(data.ByBackend)))
	sb.WriteString("</div>\n")

	// Endpoints table
	sb.WriteString(`<div class="card"><h2>Endpoints</h2><div class="table-wrap"><table>
<tr><th>Endpoint</th><th>Status</th><th>Requests</th><th>Failures</th><th>Models</th><th>Last Req</th><th>Last Fail</th><th>Fail Reason</th></tr>
`)
	// Sort endpoint names for consistent display
	epNames := make([]string, 0, len(data.Endpoints))
	for name := range data.Endpoints {
		epNames = append(epNames, name)
	}
	sort.Strings(epNames)
	for _, name := range epNames {
		ep := data.Endpoints[name]
		cls := "green"
		if ep.Status == "disabled" {
			cls = "red"
		} else if ep.Status == "offline" {
			cls = "gray"
		}
		var modelsHTML string
		if len(ep.SupportedModels) > 0 {
			var mb strings.Builder
			mb.WriteString(`<div class="models">`)
			for _, m := range ep.SupportedModels {
				mb.WriteString(fmt.Sprintf(`<span class="model-tag">%s</span>`, htmlEscape(m)))
			}
			mb.WriteString(`</div>`)
			modelsHTML = mb.String()
		} else {
			modelsHTML = "—"
		}
		failReason := ep.LastFailureReason
		if failReason == "" {
			failReason = ep.LastDiscoveryError
		}
		sb.WriteString(fmt.Sprintf(`<tr><td><code>%s</code></td><td><span class="badge %s">%s</span></td><td>%d</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td title="%s">%s</td></tr>
`, name, cls, ep.Status, ep.Requests, ep.Failures, modelsHTML, formatTime(ep.LastRequestTime), formatTime(ep.LastFailureTime), htmlEscape(failReason), truncateFailureReason(failReason, 100)))
	}
	sb.WriteString("</table></div></div>\n")

	// Access Latency card
	if len(data.ByBackend) > 0 {
		sb.WriteString(`<div class="card"><h2>Access Latency</h2>`)

		// Group by FrontendModel
		modelGroups := make(map[string][]metrics.BackendStats)
		for _, b := range data.ByBackend {
			modelGroups[b.FrontendModel] = append(modelGroups[b.FrontendModel], b)
		}

		modelNames := make([]string, 0, len(modelGroups))
		for name := range modelGroups {
			modelNames = append(modelNames, name)
		}
		sort.Strings(modelNames)

		for _, modelName := range modelNames {
			entries := modelGroups[modelName]
			sb.WriteString(fmt.Sprintf(`<div class="model-header"><code>%s</code></div>`, htmlEscape(modelName)))
			sb.WriteString(fmt.Sprintf(`<div class="table-wrap" style="margin-bottom:12px"><table>
<tr><th>Backend</th><th>Requests</th><th>Min (ms)</th><th>Max (ms)</th><th>Avg (ms)</th></tr>
`))

			var totalReqs int64
			var totalTimeMs float64
			var minMs, maxMs, avgMs float64

			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Endpoint < entries[j].Endpoint
			})

			for _, b := range entries {
				totalReqs += b.Latency.Count
				totalTimeMs += b.Latency.AvgMs * float64(b.Latency.Count)
				sb.WriteString(fmt.Sprintf(`<tr><td><code>%s</code></td><td>%d</td><td>%.1f</td><td>%.1f</td><td>%.1f</td></tr>
`, b.Endpoint, b.Latency.Count, b.Latency.MinMs, b.Latency.MaxMs, b.Latency.AvgMs))
			}

			// Summary line
			if totalReqs > 0 {
				avgMs = totalTimeMs / float64(totalReqs)
				minMs = entries[0].Latency.MinMs
				maxMs = entries[0].Latency.MaxMs
				for _, b := range entries {
					if b.Latency.MinMs < minMs {
						minMs = b.Latency.MinMs
					}
					if b.Latency.MaxMs > maxMs {
						maxMs = b.Latency.MaxMs
					}
				}
			}

			sb.WriteString(fmt.Sprintf(`</table>
<div style="display:flex;gap:16px;padding:6px 8px;border-top:1px solid #334155;font-size:12px">
<span><strong>Total:</strong> %d reqs</span>
<span><strong>Min:</strong> %.1f ms</span>
<span><strong>Max:</strong> %.1f ms</span>
<span><strong>Avg:</strong> %.1f ms</span>
</div>
`, totalReqs, minMs, maxMs, avgMs))
			sb.WriteString("</div>\n")
		}
		sb.WriteString("</div>\n")
	}

	sb.WriteString(`<div class="footer"><a href="/metrics">Prometheus Metrics</a> <span>|</span> <a href="https://github.com/2012geek/anthropic-transparent-proxy">GitHub</a></div>
</div>
</body>
</html>
`)

	fmt.Fprint(w, sb.String())
}
