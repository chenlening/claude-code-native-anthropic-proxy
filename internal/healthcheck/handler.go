package healthcheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/anthropic-transparent-proxy/internal/config"
	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/memory"
	"github.com/anthropic-transparent-proxy/internal/metrics"
	"github.com/anthropic-transparent-proxy/internal/usage"
)

// HealthResponse is the JSON response for /health endpoint
type HealthResponse struct {
	Status        string                              `json:"status"`
	TotalRequests int64                               `json:"total_requests"`
	Endpoints     map[string]metrics.EndpointSnapshot `json:"endpoints"`
	Models        map[string]metrics.ModelSnapshot    `json:"models"`
	ByBackend     []metrics.BackendStats              `json:"by_backend"`
	Memory        *MemorySnapshot                     `json:"memory,omitempty"`
	Usage         map[string]*usage.UsageData         `json:"usage,omitempty"`
}

// MemorySnapshot holds memory system stats.
type MemorySnapshot struct {
	Total       int64     `json:"total"`
	Error       string    `json:"error,omitempty"`
	LastUpdated time.Time `json:"last_updated"`
}

// Handler handles /health requests
type Handler struct {
	healthMgr    *endpoint.HealthManager
	metrics      *metrics.Metrics
	memoryClient *memory.Client
	cfg          *config.Config
	usageFetcher *usage.Fetcher
}

// NewHandler creates a new health handler
func NewHandler(hm *endpoint.HealthManager, m *metrics.Metrics, mc *memory.Client, cfg *config.Config, uf *usage.Fetcher) *Handler {
	return &Handler{healthMgr: hm, metrics: m, memoryClient: mc, cfg: cfg, usageFetcher: uf}
}

// ServeHTTP handles the health check request
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Memory page (standalone, no auto-refresh)
	if r.URL.Path == "/memory" || r.URL.Path == "/memory/" {
		h.handleMemoryPage(w, r)
		return
	}

	// OpenMemory panel reverse proxy (same-origin iframe access)
	if h.isOpenMemoryProxyPath(r.URL.Path) {
		h.handleOpenMemoryProxy(w, r)
		return
	}

	// Memory search endpoint
	if r.URL.Path == "/health/memory/search" {
		h.handleMemorySearch(w, r)
		return
	}

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

	// Memory stats
	resp.Memory = h.buildMemorySnapshot()

	// Usage data
	if h.usageFetcher != nil {
		resp.Usage = h.usageFetcher.GetAll()
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

func (h *Handler) buildMemorySnapshot() *MemorySnapshot {
	if h.memoryClient == nil {
		return nil
	}
	s := h.memoryClient.Stats()
	return &MemorySnapshot{
		Total:       s.Total,
		Error:       s.Error,
		LastUpdated: s.LastUpdated,
	}
}

func (h *Handler) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	if h.memoryClient == nil {
		http.Error(w, "memory not configured", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	results := h.memoryClient.Search(query, 10, 0.5)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (h *Handler) isOpenMemoryProxyPath(path string) bool {
	if strings.HasPrefix(path, "/_next/") ||
		strings.HasPrefix(path, "/openmemory-api/") ||
		strings.HasPrefix(path, "/images/") ||
		path == "/logo.svg" {
		return true
	}
	// OpenMemory page routes proxied for iframe client-side navigation
	for _, p := range []string{"/memories", "/apps", "/settings", "/memory"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	if strings.HasPrefix(path, "/memory-panel") {
		return true
	}
	return false
}

func (h *Handler) handleOpenMemoryProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse("http://localhost:3000")
	if err != nil {
		http.Error(w, "memory panel misconfigured", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	// Strip /memory-panel prefix so the upstream sees clean paths
	if strings.HasPrefix(r.URL.Path, "/memory-panel") {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/memory-panel")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.URL.RawPath = ""
	}
	proxy.ServeHTTP(w, r)
}

func (h *Handler) handleMemoryPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Memory Search</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#e2e8f0;overflow:hidden;height:100vh}
.search-bar{position:fixed;top:0;left:0;right:0;z-index:100;background:rgba(15,23,42,0.85);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px);padding:12px 20px;border-bottom:1px solid rgba(51,65,85,0.5);display:flex;align-items:center;justify-content:center;gap:10px}
.search-bar input{width:500px;max-width:60vw;background:#1e293b;border:1px solid #475569;color:#e2e8f0;padding:8px 14px;border-radius:8px;font-size:14px;outline:none;transition:border-color .2s}
.search-bar input:focus{border-color:#3b82f6}
.search-bar button{background:#3b82f6;color:#fff;border:none;padding:8px 16px;border-radius:8px;cursor:pointer;font-size:13px;font-weight:500;transition:background .2s;flex-shrink:0}
.search-bar button:hover{background:#2563eb}
.search-bar .close-btn{background:transparent;color:#94a3b8;font-size:18px;padding:4px 8px}
.search-bar .close-btn:hover{background:rgba(255,255,255,0.05)}
.results-panel{position:fixed;top:56px;left:50%;transform:translateX(-50%);width:700px;max-width:90vw;z-index:99;background:rgba(15,23,42,0.95);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px);border:1px solid #334155;border-top:none;border-radius:0 0 8px 8px;padding:0;display:none;max-height:300px;overflow-y:auto}
.results-panel.open{display:block}
.results-panel .result{padding:10px 20px;border-bottom:1px solid #1e293b;cursor:pointer;transition:background .15s}
.results-panel .result:hover{background:rgba(59,130,246,0.1)}
.results-panel .result .score{display:inline-block;background:#3b82f6;color:#fff;padding:1px 6px;border-radius:4px;font-size:11px;margin-right:8px;font-weight:600}
.results-panel .result .memory{color:#e2e8f0;font-size:13px}
.results-panel .empty{text-align:center;padding:24px;color:#64748b;font-size:13px}
.iframe-wrap{position:fixed;top:56px;left:0;right:0;bottom:0}
.iframe-wrap iframe{width:100%;height:100%;border:none}
.footer-link{position:fixed;bottom:8px;right:16px;z-index:101}
.footer-link a{color:#475569;text-decoration:none;font-size:11px}
.footer-link a:hover{color:#60a5fa}
.toast{position:fixed;top:64px;left:50%;transform:translateX(-50%);background:#22c55e;color:#fff;padding:6px 16px;border-radius:6px;font-size:12px;z-index:200;opacity:0;transition:opacity .3s;pointer-events:none}
.toast.show{opacity:1}
</style>
</head>
<body>
<div class="search-bar">
	<input type="text" id="mem-query" placeholder="Search memories semantically..." autofocus>
	<button onclick="doSearch()">Search</button>
	<button class="close-btn" onclick="clearResults()" id="close-btn" style="display:none">&#10005;</button>
</div>
<div class="results-panel" id="results-panel">
	<div id="results-list"></div>
</div>
<div class="toast" id="toast"></div>
<div class="iframe-wrap">
	<iframe src="/memory-panel/" id="mem-iframe" title="OpenMemory Panel" loading="lazy"></iframe>
</div>
<div class="footer-link"><a href="/health">Proxy Health</a></div>
<script>
var debounceTimer;
var input = document.getElementById('mem-query');
var panel = document.getElementById('results-panel');
var list = document.getElementById('results-list');
var closeBtn = document.getElementById('close-btn');
var iframe = document.getElementById('mem-iframe');

input.addEventListener('input', function() {
	clearTimeout(debounceTimer);
	if (!this.value.trim()) { clearResults(); return; }
	debounceTimer = setTimeout(doSearch, 300);
});

input.addEventListener('keydown', function(e) {
	if (e.key === 'Escape') clearResults();
});

document.addEventListener('click', function(e) {
	if (!e.target.closest('.search-bar') && !e.target.closest('.results-panel')) {
		clearResults();
	}
});

async function doSearch() {
	var q = input.value.trim();
	if (!q) return;
	list.innerHTML = '<div class="empty">searching...</div>';
	panel.classList.add('open');
	closeBtn.style.display = '';
	try {
		var res = await fetch('/health/memory/search?q=' + encodeURIComponent(q));
		var data = await res.json();
		if (!data.length) {
			list.innerHTML = '<div class="empty">no results</div>';
			return;
		}
		list.innerHTML = data.map(function(m) {
			return '<div class="result" data-memory="' + escapeAttr(m.memory) + '"><span class="score">' + m.score.toFixed(2) + '</span><span class="memory">' + escapeHtml(m.memory) + '</span></div>';
		}).join('');
		// Bind click handlers to results
		var items = list.querySelectorAll('.result');
		for (var i = 0; i < items.length; i++) {
			items[i].addEventListener('click', handleResultClick);
		}
	} catch(e) {
		list.innerHTML = '<div class="empty" style="color:#ef4444">error: ' + e.message + '</div>';
	}
}

function handleResultClick(e) {
	var el = e.currentTarget;
	var memoryText = el.getAttribute('data-memory');
	searchInIframe(memoryText);
	clearResults();
}

function searchInIframe(text) {
	try {
		var doc = iframe.contentDocument || iframe.contentWindow.document;
		var searchInput = doc.querySelector('input[placeholder="Search memories..."]');
		if (searchInput) {
			// Set value using native input setter to trigger React's change detection
			var nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
			nativeInputValueSetter.call(searchInput, text);
			searchInput.dispatchEvent(new Event('input', { bubbles: true }));
			showToast('Searched in panel: ' + text.substring(0, 40) + (text.length > 40 ? '...' : ''));
		} else {
			showToast('Panel not loaded yet, please wait');
		}
	} catch(e) {
		showToast('Could not access panel: ' + e.message);
	}
}

function showToast(msg) {
	var toast = document.getElementById('toast');
	toast.textContent = msg;
	toast.classList.add('show');
	setTimeout(function() { toast.classList.remove('show'); }, 2000);
}

function clearResults() {
	panel.classList.remove('open');
	list.innerHTML = '';
	closeBtn.style.display = 'none';
}

function escapeHtml(s) {
	return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

function escapeAttr(s) {
	return s.replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}
</script>
</body>
</html>`)

	fmt.Fprint(w, sb.String())
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

func renderUsageCell(epName string, ud *usage.UsageData) (cell string, detail string) {
	if ud.Error != "" {
		cell = fmt.Sprintf(`<td class="usage-error" title="%s">⚠ err</td>`, htmlEscape(ud.Error))
		detail = renderUsageDetailRow(epName, ud)
		return
	}

	color := "#22c55e"
	if ud.Provider == "zhipu" || ud.Provider == "volces" {
		pct := 0.0
		for _, d := range ud.Details {
			if d.Type == "TOKENS_LIMIT" && d.Window == "5h" {
				pct = d.Percentage
				break
			}
		}
		if pct == 0 && len(ud.Details) > 0 {
			pct = ud.Details[0].Percentage
		}
		if pct > 80 {
			color = "#ef4444"
		} else if pct > 50 {
			color = "#f59e0b"
		}
		cell = fmt.Sprintf(`<td class="usage-cell" onclick="toggleUsage('%s')" style="color:%s">%s</td>`,
			epName, color, fmt.Sprintf("%.0f%%", pct))
	} else if ud.Provider == "deepseek" {
		if len(ud.Details) > 0 && ud.Details[0].Balance != "" {
			bal := 0.0
			fmt.Sscanf(ud.Details[0].Balance, "%f", &bal)
			if bal < 1 {
				color = "#ef4444"
			} else if bal < 10 {
				color = "#f59e0b"
			}
		}
		cell = fmt.Sprintf(`<td class="usage-cell" onclick="toggleUsage('%s')" style="color:%s">%s</td>`,
			epName, color, htmlEscape(ud.Summary))
	} else {
		cell = fmt.Sprintf(`<td class="usage-cell" onclick="toggleUsage('%s')">%s</td>`,
			epName, htmlEscape(ud.Summary))
	}

	detail = renderUsageDetailRow(epName, ud)
	return
}

func renderUsageDetailRow(epName string, ud *usage.UsageData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<tr class="usage-detail" id="usage-%s"><td colspan="9"><div style="display:flex;flex-direction:column;gap:4px">`,
		epName))

	if ud.Error != "" {
		sb.WriteString(fmt.Sprintf(`<div class="usage-error">Error: %s</div>`, htmlEscape(ud.Error)))
	}

	if ud.PlanLevel != "" {
		sb.WriteString(fmt.Sprintf(`<div><span class="label">Plan:</span> <span class="value">%s</span></div>`, htmlEscape(ud.PlanLevel)))
	}

	for _, d := range ud.Details {
		sb.WriteString(`<div><span class="label">`)
		switch d.Type {
		case "TOKENS_LIMIT":
			sb.WriteString(htmlEscape(d.Window + " tokens"))
		case "TIME_LIMIT":
			sb.WriteString(htmlEscape(d.Window + " time"))
		case "BALANCE":
			sb.WriteString("Balance")
		default:
			sb.WriteString(htmlEscape(d.Type))
		}
		sb.WriteString(`:</span> <span class="value">`)

		if d.Type == "BALANCE" {
			symbol := "¥"
			if d.Currency == "USD" {
				symbol = "$"
			}
			sb.WriteString(fmt.Sprintf(`%s%s %s`, symbol, htmlEscape(d.Balance), d.Currency))
			if d.IsAvailable != nil {
				if *d.IsAvailable {
					sb.WriteString(` <span style="color:#22c55e">available</span>`)
				} else {
					sb.WriteString(` <span style="color:#ef4444">unavailable</span>`)
				}
			}
		} else if d.Total > 0 {
			sb.WriteString(fmt.Sprintf(`%d / %d (%.1f%%)`, d.Used, d.Total, d.Percentage))
		} else {
			sb.WriteString(fmt.Sprintf(`%.1f%%`, d.Percentage))
		}

		if !d.ResetTime.IsZero() {
			sb.WriteString(fmt.Sprintf(` <span style="color:#64748b">resets %s</span>`, d.ResetTime.Format("2006-01-02 15:04")))
		}
		if len(d.PerModel) > 0 {
			sb.WriteString(` <span style="color:#64748b;font-size:11px">| `)
			for i, m := range d.PerModel {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf(`%s: %d`, htmlEscape(m.Model), m.Usage))
			}
			sb.WriteString(`</span>`)
		}
		sb.WriteString(`</span></div>`)
	}

	if !ud.FetchedAt.IsZero() {
		stale := ""
		if ud.IsStale(30 * time.Minute) {
			stale = ` <span class="usage-stale">(stale)</span>`
		}
		sb.WriteString(fmt.Sprintf(`<div style="margin-top:4px"><span class="label">Fetched:</span> <span class="value">%s</span>%s</div>`,
			ud.FetchedAt.Format("2006-01-02 15:04:05"), stale))
	}

	sb.WriteString(`</div></td></tr>`)
	return sb.String()
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
table{width:100%;border-collapse:collapse;table-layout:fixed}
th,td{padding:6px 8px;border-bottom:1px solid #334155;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
th{white-space:normal;overflow:visible}
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
.search-form input{background:#334155;border:1px solid #475569;color:#e2e8f0;padding:6px 10px;border-radius:4px;width:300px;font-size:13px}
.search-form button{background:#3b82f6;color:#fff;border:none;padding:6px 14px;border-radius:4px;cursor:pointer;font-size:13px;margin-left:6px}
.search-results{margin-top:10px;font-size:13px}
.search-results .result{padding:6px 0;border-bottom:1px solid #334155}
.search-results .score{color:#64748b;font-size:11px}
.usage-cell{cursor:pointer;min-width:80px}
.usage-detail{display:none;background:#0f172a}
.usage-detail.open{display:table-row}
.usage-detail td{padding:10px 12px;font-size:12px;color:#94a3b8;overflow:visible;white-space:normal}
.usage-detail .label{color:#64748b;min-width:100px}
.usage-detail .value{color:#e2e8f0}
.usage-stale{color:#f59e0b;font-size:10px}
.usage-error{color:#ef4444;font-size:12px}
.models-td{white-space:normal;overflow:visible}
</style>
</head>
<body>
<div class="container">
`)

	// Status header
	sb.WriteString(fmt.Sprintf(`<h1><span class="status-dot" style="color:%s">●</span> Proxy %s</h1>
`, statusColor, strings.ToUpper(data.Status[:1])+data.Status[1:]))
	sb.WriteString(fmt.Sprintf(`<p class="timestamp">Last updated: %s | Auto-refreshes every 60s</p>
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
	if data.Memory != nil {
		if data.Memory.Error != "" {
			sb.WriteString(fmt.Sprintf(`<div class="stat-item" title="%s"><div class="stat-value" style="color:#f59e0b">err</div><div class="stat-label">Memories</div></div>
`, htmlEscape(data.Memory.Error)))
		} else {
			sb.WriteString(fmt.Sprintf(`<div class="stat-item"><a href="/memory" style="color:#60a5fa;text-decoration:none"><div class="stat-value">%d</div></a><div class="stat-label">Memories</div></div>
`, data.Memory.Total))
		}
	}
	sb.WriteString("</div>\n")

	// Endpoints table
	sb.WriteString(`<div class="card"><h2>Endpoints</h2><div class="table-wrap"><table>
<tr><th style="width:11%">Endpoint</th><th style="width:7%">Status</th><th style="width:7%">Requests</th><th style="width:7%">Failures</th><th style="width:24%">Models</th><th style="width:8%">Usage</th><th style="width:12%">Last Success</th><th style="width:12%">Last Fail</th><th style="width:12%">Fail Reason</th></tr>
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
		// Usage cell
		var usageHTML string
		var usageDetailHTML string
		if data.Usage != nil {
			if ud, ok := data.Usage[name]; ok {
				usageHTML, usageDetailHTML = renderUsageCell(name, ud)
			}
		}
		if usageHTML == "" {
			usageHTML = `<td>—</td>`
		}
		sb.WriteString(fmt.Sprintf(`<tr><td><code>%s</code></td><td><span class="badge %s">%s</span></td><td>%d</td><td>%d</td><td class="models-td">%s</td>%s<td>%s</td><td>%s</td><td title="%s">%s</td></tr>
`, name, cls, ep.Status, ep.Requests, ep.Failures, modelsHTML, usageHTML, formatTime(ep.LastRequestTime), formatTime(ep.LastFailureTime), htmlEscape(failReason), truncateFailureReason(failReason, 100)))
		if usageDetailHTML != "" {
			sb.WriteString(usageDetailHTML)
		}
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
<tr><th style="width:30%%">Backend</th><th style="width:18%%">Requests</th><th style="width:18%%">Min (ms)</th><th style="width:18%%">Max (ms)</th><th style="width:16%%">Avg (ms)</th></tr>
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

	sb.WriteString(`<div class="card"><h2>Memory</h2>
<div style="text-align:center;padding:8px 0">
<a href="/memory" style="color:#60a5fa;text-decoration:none;font-size:14px;font-weight:500">Open Memory Search</a>
<span style="color:#64748b;font-size:12px;margin-left:8px">semantic search + panel</span>
</div>
`)
	if data.Memory != nil && !data.Memory.LastUpdated.IsZero() {
		age := time.Since(data.Memory.LastUpdated).Truncate(time.Second)
		sb.WriteString(fmt.Sprintf(`<div style="display:flex;justify-content:center;gap:16px;padding:4px 8px;font-size:12px;color:#64748b">
<span>cached: %d memories</span><span>refreshed %s ago</span>
</div>
`, data.Memory.Total, age))
	}
	sb.WriteString(`</div>

	<div class="footer"><a href="/metrics">Prometheus Metrics</a> <span>|</span> <a href="/memory">Memory Search</a> <span>|</span> <a href="https://github.com/2012geek/anthropic-transparent-proxy">GitHub</a></div>
</div>
<script>
var refreshTimer;
function hasOpenUsage() {
	return document.querySelectorAll('.usage-detail.open').length > 0;
}
function scheduleRefresh() {
	refreshTimer = setTimeout(function() {
		if (hasOpenUsage()) { scheduleRefresh(); return; }
		location.reload();
	}, 60000);
}
function toggleUsage(name) {
	var el = document.getElementById('usage-' + name);
	if (el) el.classList.toggle('open');
	if (!hasOpenUsage() && !refreshTimer) scheduleRefresh();
}
scheduleRefresh();
</script>
</body>
</html>
`)

	fmt.Fprint(w, sb.String())
}
