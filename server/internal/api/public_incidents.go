package api

import (
	"html/template"
	"net/http"
)

type publicIncidentItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Priority    string   `json:"priority"`
	PlayerURL   string   `json:"playerUrl,omitempty"`
	Talkgroups  []int    `json:"talkgroups,omitempty"`
	OpenedAt    int64    `json:"openedAt"`
}

func (h *handler) fetchPublicIncidents() []publicIncidentItem {
	incidents, err := h.db.ListActiveCommunityIncidents()
	if err != nil {
		h.logger.Error("list active community incidents failed", "error", err)
		return nil
	}
	out := make([]publicIncidentItem, 0, len(incidents))
	for _, inc := range incidents {
		item := publicIncidentItem{
			ID:       inc.ID,
			Title:    inc.Title,
			Type:     inc.IncidentType,
			Priority: inc.Priority,
			OpenedAt: inc.OpenedAt,
		}
		playerURL := h.incidentPublicPlayerURL(inc)
		if playerURL != "" {
			item.PlayerURL = playerURL
			if rs, found, rsErr := h.db.GetRadioSetForPTT(inc.RadioSetID); rsErr == nil && found {
				item.Talkgroups = rs.Talkgroups
			}
		}
		out = append(out, item)
	}
	return out
}

func (h *handler) handlePublicIncidentsJSON(w http.ResponseWriter, r *http.Request) {
	items := h.fetchPublicIncidents()
	if items == nil {
		items = []publicIncidentItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

type publicIncidentsPageData struct {
	Items []publicIncidentItem
}

var publicIncidentsTmpl = template.Must(template.New("public-incidents").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="theme-color" content="#0a0a0a">
<title>Active Incidents // SignalForge Hub</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%;background:#0a0a0a;color:#c9c9c9;font-family:'JetBrains Mono','Courier New',Courier,monospace}
body{display:flex;flex-direction:column;align-items:center;padding:2rem 1rem}
h1{font-size:13px;color:#ffaa00;text-transform:uppercase;letter-spacing:.16em;margin-bottom:.25rem}
.sub{font-size:10px;color:#555;margin-bottom:1.5rem;letter-spacing:.08em}
.list{width:100%;max-width:640px;display:flex;flex-direction:column;gap:.5rem}
.card{border:1px solid #1f1f1f;border-radius:4px;padding:1rem;background:#111;display:flex;flex-direction:column;gap:.35rem}
.card-title{font-size:13px;color:#ffaa00;font-weight:700;letter-spacing:.03em}
.card-meta{font-size:9px;color:#555;text-transform:uppercase;letter-spacing:.1em}
.card-tgs{font-size:10px;color:#c9c9c9}
.card-link{margin-top:.25rem}
.card-link a{font-size:10px;color:#ffc700;letter-spacing:.06em;text-decoration:none;border:1px solid #ffaa00;border-radius:2px;padding:.3rem .6rem;display:inline-block;transition:background .1s}
.card-link a:hover{background:rgba(255,170,0,.1)}
.empty{font-size:11px;color:#555;text-align:center;padding:2rem}
</style>
</head>
<body>
<h1>Active Incidents</h1>
<p class="sub">SignalForge Hub &mdash; public incident list</p>
<div class="list">
{{if .Items}}
  {{range .Items}}
  <div class="card">
    <div class="card-title">{{.Title}}</div>
    <div class="card-meta">{{.Type}} · {{.Priority}} priority</div>
    {{if .TalkgroupLabel}}<div class="card-tgs">{{.TalkgroupLabel}}</div>{{end}}
    {{if .PlayerURL}}<div class="card-link"><a href="{{.PlayerURL}}" target="_blank" rel="noopener">▶ LISTEN LIVE</a></div>{{end}}
  </div>
  {{end}}
{{else}}
  <div class="empty">No active community incidents right now.</div>
{{end}}
</div>
</body>
</html>`))

func (h *handler) handlePublicIncidentsPage(w http.ResponseWriter, r *http.Request) {
	items := h.fetchPublicIncidents()
	if items == nil {
		items = []publicIncidentItem{}
	}
	w.Header().Set("Cache-Control", "no-cache")
	if err := publicIncidentsTmpl.Execute(w, publicIncidentsPageData{Items: items}); err != nil {
		h.logger.Error("public incidents template render failed", "error", err)
	}
}
