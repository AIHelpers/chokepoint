package api

import "net/http"

// RegisterDashboard mounts a minimal, dependency-free HTML dashboard
// at "/" that reads from the JSON API client-side. It exists so the
// platform is usable out of the box without standing up a separate
// frontend project; teams that want a richer UI can build against the
// same /api/* endpoints.
func (a *API) RegisterDashboard(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.handleDashboard)
}

func (a *API) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Chokepoint — LLM Governance Dashboard</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: -apple-system, Segoe UI, sans-serif; margin: 2rem; max-width: 1100px; }
  h1 { font-size: 1.4rem; }
  .cards { display: flex; gap: 1rem; flex-wrap: wrap; margin: 1.5rem 0; }
  .card { border: 1px solid #8884; border-radius: 8px; padding: 1rem 1.25rem; min-width: 160px; }
  .card .label { font-size: .8rem; opacity: .7; }
  .card .value { font-size: 1.6rem; font-weight: 600; }
  table { border-collapse: collapse; width: 100%; font-size: .85rem; }
  th, td { text-align: left; padding: .4rem .6rem; border-bottom: 1px solid #8882; }
  .badge { padding: .1rem .5rem; border-radius: 999px; font-size: .75rem; }
  .badge.block { background: #fde2e2; color: #b42318; }
  .badge.redact { background: #fef3c7; color: #92400e; }
  .badge.allow { background: #dcfce7; color: #166534; }
  .badge.route { background: #dbeafe; color: #1e40af; }
</style>
</head>
<body>
  <h1>🛡️ Chokepoint — LLM Call Governance &amp; Observability</h1>
  <div class="cards" id="cards"></div>
  <h2>Recent calls</h2>
  <table id="logs">
    <thead>
      <tr><th>Time</th><th>Team</th><th>Feature</th><th>Model</th><th>Tokens</th><th>Cost</th><th>Latency</th><th>Policy</th></tr>
    </thead>
    <tbody></tbody>
  </table>
<script>
async function load() {
  const [stats, logs] = await Promise.all([
    fetch('/api/stats').then(r => r.json()),
    fetch('/api/logs?limit=25').then(r => r.json()),
  ]);

  const cards = document.getElementById('cards');
  var cardDefs = [
    ['Total calls', stats.total_calls],
    ['Total cost', '$' + (stats.total_cost_usd || 0).toFixed(4)],
    ['Errors', stats.error_count],
    ['Avg latency', Math.round(stats.avg_latency_ms || 0) + ' ms'],
    ['Blocked', stats.blocked_count],
    ['Redacted', stats.redacted_count],
  ];
  cards.innerHTML = cardDefs.map(function(pair) {
    return '<div class="card"><div class="label">' + pair[0] + '</div><div class="value">' + pair[1] + '</div></div>';
  }).join('');

  const tbody = document.querySelector('#logs tbody');
  tbody.innerHTML = (logs || []).map(function(r) {
    return '<tr>' +
      '<td>' + new Date(r.timestamp).toLocaleTimeString() + '</td>' +
      '<td>' + (r.team || '\u2014') + '</td>' +
      '<td>' + (r.feature || '\u2014') + '</td>' +
      '<td>' + (r.model || '\u2014') + '</td>' +
      '<td>' + (r.prompt_tokens||0) + '/' + (r.completion_tokens||0) + '</td>' +
      '<td>$' + (r.cost_usd||0).toFixed(4) + '</td>' +
      '<td>' + (r.latency_ms||0) + ' ms</td>' +
      '<td><span class="badge ' + r.policy_action + '">' + r.policy_action + '</span></td>' +
      '</tr>';
  }).join('');
}
load();
setInterval(load, 5000);
</script>
</body>
</html>`
