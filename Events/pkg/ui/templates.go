package ui

import (
	"encoding/json"
	"events/pkg/models"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
)

// Common CSS shared across all merchant pages.
const baseCSS = `
:root {
  --bg-dark: #0a0e14;
  --bg-card: rgba(22, 28, 38, 0.75);
  --bg-card-hover: rgba(30, 38, 52, 0.9);
  --border-subtle: rgba(255, 255, 255, 0.08);
  --border-glow: rgba(0, 240, 255, 0.3);
  --accent-cyan: #00f0ff;
  --accent-blue: #0066ff;
  --accent-violet: #8a2be2;
  --accent-amber: #ffaa00;
  --accent-emerald: #00e676;
  --text-main: #f0f4fc;
  --text-muted: #8b9bb4;
  --text-dim: #5c6b82;
  --glass: blur(16px);
}

* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  background-color: var(--bg-dark);
  background-image: 
    radial-gradient(circle at 15% 15%, rgba(0, 102, 255, 0.08) 0%, transparent 40%),
    radial-gradient(circle at 85% 75%, rgba(138, 43, 226, 0.08) 0%, transparent 40%);
  color: var(--text-main);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  line-height: 1.5;
  min-height: 100vh;
}

/* Sandbox Banner */
.sandbox-banner {
  background: linear-gradient(90deg, #ffaa00 0%, #ff7700 100%);
  color: #0a0e14;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding: 8px 16px;
  text-align: center;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
}
.sandbox-banner span {
  background: rgba(0,0,0,0.15);
  padding: 2px 8px;
  border-radius: 4px;
}

/* Navigation Bar */
header.nav-header {
  border-bottom: 1px solid var(--border-subtle);
  background: rgba(10, 14, 20, 0.85);
  backdrop-filter: var(--glass);
  position: sticky;
  top: 0;
  z-index: 100;
}
.nav-container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 16px 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  text-decoration: none;
  color: var(--text-main);
}
.brand-logo {
  width: 34px;
  height: 34px;
  background: linear-gradient(135deg, var(--accent-cyan), var(--accent-blue));
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 900;
  font-size: 18px;
  color: #000;
  box-shadow: 0 0 16px rgba(0, 240, 255, 0.4);
}
.brand-text {
  font-size: 18px;
  font-weight: 800;
  letter-spacing: -0.02em;
}
.brand-text span {
  color: var(--accent-cyan);
}
.nav-links {
  display: flex;
  align-items: center;
  gap: 20px;
}
.nav-links a {
  color: var(--text-muted);
  text-decoration: none;
  font-size: 14px;
  font-weight: 600;
  transition: color 0.2s;
}
.nav-links a:hover, .nav-links a.active {
  color: var(--accent-cyan);
}
.nav-btn {
  background: rgba(0, 240, 255, 0.1);
  border: 1px solid rgba(0, 240, 255, 0.3);
  color: var(--accent-cyan) !important;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 13px !important;
  font-weight: 700 !important;
  transition: all 0.2s;
}
.nav-btn:hover {
  background: var(--accent-cyan);
  color: #000 !important;
  box-shadow: 0 0 16px rgba(0, 240, 255, 0.3);
}

/* Page Container */
.container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 32px 24px;
}

/* Badges */
.badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.badge-cyan { background: rgba(0, 240, 255, 0.12); color: var(--accent-cyan); border: 1px solid rgba(0, 240, 255, 0.25); }
.badge-violet { background: rgba(138, 43, 226, 0.15); color: #c084fc; border: 1px solid rgba(138, 43, 226, 0.3); }
.badge-emerald { background: rgba(0, 230, 118, 0.12); color: var(--accent-emerald); border: 1px solid rgba(0, 230, 118, 0.25); }
.badge-amber { background: rgba(255, 170, 0, 0.15); color: var(--accent-amber); border: 1px solid rgba(255, 170, 0, 0.3); }

/* Buttons */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 12px 24px;
  border-radius: 10px;
  font-size: 15px;
  font-weight: 700;
  text-decoration: none;
  cursor: pointer;
  border: none;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.btn-primary {
  background: linear-gradient(135deg, var(--accent-cyan) 0%, var(--accent-blue) 100%);
  color: #050a12;
  box-shadow: 0 4px 20px rgba(0, 240, 255, 0.25);
}
.btn-primary:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(0, 240, 255, 0.4);
}
.btn-secondary {
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid var(--border-subtle);
  color: var(--text-main);
}
.btn-secondary:hover {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.2);
}
`

// View structs for template rendering

type HomeView struct {
	Events   []*models.Event
	Category string
	Search   string
}

type EventView struct {
	Event   *models.Event
	JSONLD  template.JS
	Host    string
	BaseURL string
}

type TicketsView struct {
	Event   *models.Event
	JSONLD  template.JS
	Host    string
	BaseURL string
}

type TicketPassView struct {
	Found         bool
	Order         *models.OrderConfirmation
	FormattedDate string
	FormattedTime string
	JSONLD        template.JS
}

type DashboardView struct {
	DemoKey         string
	Scenario        string
	Orders          []*models.OrderConfirmation
	Rejected        []models.RejectedRequest
	TotalOrders     int
	TotalGrossCents int
	Host            string
}

var funcMap = template.FuncMap{
	"centsToDollars": func(cents int) string {
		return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
	},
	"formatDate": func(t time.Time) string {
		return t.Format("Mon, Jan 2, 2006")
	},
	"formatTime": func(t time.Time) string {
		return t.Format("3:04 PM")
	},
	"formatDateTime": func(t time.Time) string {
		return t.Format("Mon, Jan 2 · 3:04 PM MST")
	},
	"categoryTitle": func(cat string) string {
		switch cat {
		case "live_music":
			return "Live Music"
		case "nightclub":
			return "Nightlife & Clubs"
		case "comedy":
			return "Comedy"
		case "sports_event":
			return "Sports & Games"
		case "tour":
			return "Tours & Food"
		case "class_workshop":
			return "Workshops & Classes"
		case "community_event":
			return "Community"
		default:
			return strings.ReplaceAll(cat, "_", " ")
		}
	},
}

// 1. Home Template (Event Discovery)
var homeTmpl = template.Must(template.New("home").Funcs(funcMap).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SideQuestz Events · Ticketmaster Demo Sandbox</title>
<style>
` + baseCSS + `
/* Discovery Hero */
.hero {
  padding: 48px 0 32px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}
.hero-tag {
  margin-bottom: 16px;
}
.hero h1 {
  font-size: 42px;
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1.15;
  margin-bottom: 14px;
}
.hero h1 span {
  background: linear-gradient(135deg, #00f0ff 0%, #0088ff 50%, #9945ff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}
.hero p {
  color: var(--text-muted);
  font-size: 18px;
  max-width: 640px;
  margin-bottom: 32px;
}

/* Filter Controls */
.filter-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 36px;
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  padding: 16px 20px;
  border-radius: 16px;
  border: 1px solid var(--border-subtle);
}
.categories {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.cat-pill {
  padding: 8px 16px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border-subtle);
  color: var(--text-muted);
  text-decoration: none;
  font-size: 13px;
  font-weight: 600;
  transition: all 0.2s;
}
.cat-pill:hover, .cat-pill.active {
  background: var(--accent-cyan);
  color: #000;
  border-color: var(--accent-cyan);
  box-shadow: 0 0 12px rgba(0, 240, 255, 0.3);
}
.search-form {
  display: flex;
  align-items: center;
  gap: 8px;
}
.search-input {
  background: rgba(10, 14, 20, 0.6);
  border: 1px solid var(--border-subtle);
  color: var(--text-main);
  padding: 8px 16px;
  border-radius: 10px;
  font-size: 14px;
  outline: none;
  min-width: 240px;
}
.search-input:focus {
  border-color: var(--accent-cyan);
  box-shadow: 0 0 8px rgba(0, 240, 255, 0.2);
}

/* Event Grid */
.event-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 28px;
}
.event-card {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-subtle);
  border-radius: 18px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
  text-decoration: none;
  color: inherit;
  position: relative;
}
.event-card:hover {
  transform: translateY(-6px);
  border-color: var(--border-glow);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.5), 0 0 20px rgba(0, 240, 255, 0.15);
}
.event-img {
  width: 100%;
  height: 180px;
  object-fit: cover;
  background: #141a24;
}
.event-card-body {
  padding: 20px;
  display: flex;
  flex-direction: column;
  flex: 1;
}
.event-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.event-date {
  color: var(--accent-cyan);
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.03em;
  text-transform: uppercase;
}
.event-title {
  font-size: 19px;
  font-weight: 700;
  line-height: 1.3;
  margin-bottom: 8px;
}
.event-venue {
  color: var(--text-muted);
  font-size: 14px;
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.event-footer {
  margin-top: auto;
  padding-top: 16px;
  border-top: 1px solid var(--border-subtle);
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.price-tag {
  font-size: 18px;
  font-weight: 800;
  color: var(--text-main);
}
.price-tag small {
  font-size: 12px;
  color: var(--text-dim);
  font-weight: 400;
}
</style>
</head>
<body>

<div class="sandbox-banner">
  <span>Sandbox Active</span>
  Official Ticketmaster Partner Demo · Simulated Visa Agent Network
</div>

<header class="nav-header">
  <div class="nav-container">
    <a href="/" class="brand">
      <div class="brand-logo">S</div>
      <div class="brand-text">SideQuestz <span>Events</span></div>
    </a>
    <nav class="nav-links">
      <a href="/" class="active">Discover</a>
      <a href="/dashboard">Booth Dashboard</a>
      <a href="/dashboard" class="nav-btn">Operator Screen</a>
    </nav>
  </div>
</header>

<main class="container">
  <div class="hero">
    <div class="hero-tag">
      <span class="badge badge-cyan">Saltlight Harbor City Catalog</span>
    </div>
    <h1>Live Entertainment <span>Engineered for Agents</span></h1>
    <p>Explore 25 premier concert, nightlife, comedy, and festival events. Ready for human fans and autonomous Visa Agentic Checkout.</p>
  </div>

  <div class="filter-bar">
    <div class="categories">
      <a href="/" class="cat-pill {{if eq .Category ""}}active{{end}}">All Events</a>
      <a href="/?category=live_music" class="cat-pill {{if eq .Category "live_music"}}active{{end}}">Live Music</a>
      <a href="/?category=nightclub" class="cat-pill {{if eq .Category "nightclub"}}active{{end}}">Nightlife</a>
      <a href="/?category=comedy" class="cat-pill {{if eq .Category "comedy"}}active{{end}}">Comedy</a>
      <a href="/?category=tour" class="cat-pill {{if eq .Category "tour"}}active{{end}}">Tours</a>
      <a href="/?category=sports_event" class="cat-pill {{if eq .Category "sports_event"}}active{{end}}">Sports</a>
      <a href="/?category=class_workshop" class="cat-pill {{if eq .Category "class_workshop"}}active{{end}}">Workshops</a>
    </div>
    <form class="search-form" method="GET" action="/">
      <input type="text" name="q" class="search-input" placeholder="Search events, venues..." value="{{.Search}}">
      {{if .Category}}<input type="hidden" name="category" value="{{.Category}}">{{end}}
      <button type="submit" class="btn btn-secondary" style="padding: 8px 14px;">Search</button>
    </form>
  </div>

  <div class="event-grid">
    {{range .Events}}
    <a href="/{{.Slug}}" class="event-card">
      <img src="{{.ImageURL}}" alt="{{.Title}}" class="event-img" loading="lazy">
      <div class="event-card-body">
        <div class="event-meta">
          <span class="event-date">{{formatDateTime .Start}}</span>
          <span class="badge badge-emerald">{{.Remaining}} Left</span>
        </div>
        <h2 class="event-title">{{.Title}}</h2>
        <p class="event-venue">📍 {{.Venue}}</p>
        <div class="event-footer">
          <div class="price-tag">
            {{centsToDollars .UnitCents}} <small>/ ticket</small>
          </div>
          <span class="btn btn-secondary" style="padding: 6px 14px; font-size: 13px;">View Event →</span>
        </div>
      </div>
    </a>
    {{end}}
  </div>
</main>

</body>
</html>`))

// 2. Event Detail Template
var eventTmpl = template.Must(template.New("event").Funcs(funcMap).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Event.Title}} · SideQuestz Events</title>
<script type="application/ld+json">
{{.JSONLD}}
</script>
<style>
` + baseCSS + `
.event-header {
  position: relative;
  border-radius: 24px;
  overflow: hidden;
  margin-bottom: 36px;
  background: #141a24;
  border: 1px solid var(--border-subtle);
  min-height: 380px;
  display: flex;
  align-items: flex-end;
}
.header-bg {
  position: absolute;
  top: 0; left: 0; right: 0; bottom: 0;
  width: 100%; height: 100%;
  object-fit: cover;
  opacity: 0.45;
}
.header-overlay {
  position: absolute;
  top: 0; left: 0; right: 0; bottom: 0;
  background: linear-gradient(180deg, rgba(10,14,20,0.2) 0%, rgba(10,14,20,0.95) 100%);
}
.header-content {
  position: relative;
  z-index: 2;
  padding: 40px;
  max-width: 800px;
}
.header-content h1 {
  font-size: 38px;
  font-weight: 800;
  line-height: 1.2;
  margin: 12px 0 16px;
}
.header-details {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
  color: var(--text-muted);
  font-size: 16px;
  font-weight: 500;
}
.detail-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.event-body-layout {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 36px;
}
.content-box {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-subtle);
  border-radius: 20px;
  padding: 32px;
  margin-bottom: 24px;
}
.content-box h2 {
  font-size: 22px;
  font-weight: 700;
  margin-bottom: 16px;
  color: var(--accent-cyan);
}
.desc-text {
  font-size: 17px;
  line-height: 1.6;
  color: var(--text-main);
  margin-bottom: 20px;
}

.ticket-cta-card {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-glow);
  border-radius: 20px;
  padding: 28px;
  position: sticky;
  top: 100px;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.4);
}
.cta-price {
  font-size: 32px;
  font-weight: 800;
  margin-bottom: 4px;
}
.cta-avail {
  margin-bottom: 24px;
}
@media (max-width: 860px) {
  .event-body-layout { grid-template-columns: 1fr; }
}
</style>
</head>
<body>

<div class="sandbox-banner">
  <span>Sandbox Active</span>
  Official Ticketmaster Partner Demo · Simulated Visa Agent Network
</div>

<header class="nav-header">
  <div class="nav-container">
    <a href="/" class="brand">
      <div class="brand-logo">S</div>
      <div class="brand-text">SideQuestz <span>Events</span></div>
    </a>
    <nav class="nav-links">
      <a href="/">← Back to Events</a>
      <a href="/dashboard">Booth Dashboard</a>
    </nav>
  </div>
</header>

<main class="container">
  <div class="event-header">
    <img src="{{.Event.ImageURL}}" alt="{{.Event.Title}}" class="header-bg">
    <div class="header-overlay"></div>
    <div class="header-content">
      <span class="badge badge-cyan">{{categoryTitle .Event.Category}}</span>
      <h1>{{.Event.Title}}</h1>
      <div class="header-details">
        <div class="detail-item">📅 {{formatDate .Event.Start}} at {{formatTime .Event.Start}}</div>
        <div class="detail-item">📍 {{.Event.Venue}}</div>
      </div>
    </div>
  </div>

  <div class="event-body-layout">
    <div class="main-content">
      <div class="content-box">
        <h2>About This Event</h2>
        <p class="desc-text">{{.Event.Description}}</p>
        <p style="color: var(--text-muted); font-size: 15px;">Venue Location: {{.Event.Address}}</p>
      </div>

      <div class="content-box">
        <h2>Event Tags & Experience</h2>
        <div style="display:flex; flex-wrap:wrap; gap:8px;">
          {{range .Event.Tags}}
          <span class="badge badge-violet">#{{.}}</span>
          {{end}}
        </div>
      </div>
    </div>

    <div class="sidebar">
      <div class="ticket-cta-card">
        <div class="cta-price">{{centsToDollars .Event.UnitCents}} <small style="font-size:14px; color:var(--text-muted); font-weight:normal;">/ person</small></div>
        <div class="cta-avail">
          <span class="badge badge-emerald">✓ {{.Event.Remaining}} Tickets Remaining</span>
        </div>
        <p style="font-size: 14px; color: var(--text-muted); margin-bottom: 24px;">
          Secure your admission directly through our online box office or authorize SideQuestz Muse to auto-purchase.
        </p>
        <a href="/{{.Event.Slug}}/tickets" class="btn btn-primary" style="width: 100%; text-align: center;">Get Tickets →</a>
      </div>
    </div>
  </div>
</main>

</body>
</html>`))

// 3. Ticket Page Template (Selection & Checkout)
var ticketsTmpl = template.Must(template.New("tickets").Funcs(funcMap).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tickets: {{.Event.Title}} · SideQuestz Events</title>
<link rel="agent-checkout" href="/api/events/{{.Event.Slug}}/offer">
<script type="application/ld+json">
{{.JSONLD}}
</script>
<style>
` + baseCSS + `
.ticket-selector-card {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-glow);
  border-radius: 24px;
  padding: 36px;
  max-width: 640px;
  margin: 24px auto;
  box-shadow: 0 16px 40px rgba(0,0,0,0.5);
}
.event-mini-header {
  border-bottom: 1px solid var(--border-subtle);
  padding-bottom: 20px;
  margin-bottom: 24px;
}
.event-mini-header h1 {
  font-size: 26px;
  font-weight: 800;
  margin-bottom: 6px;
}
.quantity-control {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: rgba(10, 14, 20, 0.6);
  border: 1px solid var(--border-subtle);
  padding: 16px 20px;
  border-radius: 14px;
  margin-bottom: 28px;
}
.counter-btns {
  display: flex;
  align-items: center;
  gap: 16px;
}
.counter-btn {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid var(--border-subtle);
  color: var(--text-main);
  font-size: 20px;
  font-weight: 700;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}
.counter-btn:hover {
  background: var(--accent-cyan);
  color: #000;
}
.counter-display {
  font-size: 20px;
  font-weight: 800;
  min-width: 30px;
  text-align: center;
}

/* Pricing Breakdown Table */
.price-breakdown {
  width: 100%;
  margin-bottom: 28px;
  border-collapse: collapse;
}
.price-breakdown tr td {
  padding: 10px 0;
  font-size: 15px;
  color: var(--text-muted);
}
.price-breakdown tr td:last-child {
  text-align: right;
  color: var(--text-main);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.price-breakdown tr.total-row {
  border-top: 1px dashed rgba(255,255,255,0.2);
  font-size: 18px;
}
.price-breakdown tr.total-row td {
  padding-top: 16px;
  font-weight: 800;
  color: var(--accent-cyan);
}

/* Agentic Checkout Banner */
.agent-notice-box {
  background: rgba(0, 240, 255, 0.05);
  border: 1px solid rgba(0, 240, 255, 0.2);
  border-radius: 14px;
  padding: 18px;
  margin-bottom: 28px;
  font-size: 14px;
  color: var(--text-muted);
  line-height: 1.5;
}
.agent-notice-box strong {
  color: var(--accent-cyan);
  display: block;
  margin-bottom: 4px;
}

/* Live Simulation Box */
.sim-box {
  background: rgba(138, 43, 226, 0.08);
  border: 1px solid rgba(138, 43, 226, 0.3);
  border-radius: 16px;
  padding: 20px;
  text-align: center;
}
.sim-box h3 {
  font-size: 16px;
  color: #c084fc;
  margin-bottom: 8px;
}
</style>
</head>
<body>

<div class="sandbox-banner">
  <span>Sandbox Active</span>
  Official Ticketmaster Partner Demo · Simulated Visa Agent Network
</div>

<header class="nav-header">
  <div class="nav-container">
    <a href="/" class="brand">
      <div class="brand-logo">S</div>
      <div class="brand-text">SideQuestz <span>Events</span></div>
    </a>
    <nav class="nav-links">
      <a href="/{{.Event.Slug}}">← Event Details</a>
      <a href="/dashboard">Booth Dashboard</a>
    </nav>
  </div>
</header>

<main class="container">
  <div class="ticket-selector-card">
    <div class="event-mini-header">
      <span class="badge badge-cyan">{{categoryTitle .Event.Category}}</span>
      <h1>{{.Event.Title}}</h1>
      <p style="color: var(--text-muted); font-size: 14px;">📍 {{.Event.Venue}} · {{formatDateTime .Event.Start}}</p>
      <p style="color: var(--accent-emerald); font-size: 13px; font-weight: 600; margin-top: 6px;">
        ✓ {{.Event.Remaining}} tickets available (Capacity: {{.Event.Capacity}})
      </p>
    </div>

    <div class="quantity-control">
      <div>
        <div style="font-weight: 700; font-size: 16px;">General Admission</div>
        <div style="font-size: 13px; color: var(--text-muted);">{{centsToDollars .Event.UnitCents}} per ticket</div>
      </div>
      <div class="counter-btns">
        <button class="counter-btn" id="btn-dec" onclick="updateQty(-1)">−</button>
        <span class="counter-display" id="qty-val">2</span>
        <button class="counter-btn" id="btn-inc" onclick="updateQty(1)">+</button>
      </div>
    </div>

    <table class="price-breakdown">
      <tr>
        <td>Ticket Subtotal (<span id="qty-label">2</span> × {{centsToDollars .Event.UnitCents}})</td>
        <td id="subtotal-val">$24.00</td>
      </tr>
      <tr>
        <td>Service & Processing Fees (8% + $0.50/ea)</td>
        <td id="fees-val">$3.10</td>
      </tr>
      <tr class="total-row">
        <td>Total (USD)</td>
        <td id="total-val">$27.10</td>
      </tr>
    </table>

    <div class="agent-notice-box">
      <strong>🤖 Agentic Checkout Enabled</strong>
      Tickets for this sandbox are bought by the SideQuestz agent using Visa's Trusted Agent Protocol (TAP).
      The agent reads this page's <code style="color:var(--accent-cyan);">&lt;link rel="agent-checkout"&gt;</code> to secure cryptographic quotes and book via authenticated tokens.
    </div>

    <div class="sim-box">
      <h3>Live Demo: Test Agentic Checkout</h3>
      <p style="font-size: 13px; color: var(--text-muted); margin-bottom: 16px;">
        Click below to simulate an autonomous Visa Agent purchase (generates quote, simulates TAP Ed25519 signature, authorizes single-use Visa token, and issues ticket pass).
      </p>
      <button class="btn btn-primary" id="sim-btn" onclick="runSimulatedAgentPurchase()" style="width: 100%;">
        ⚡ Simulate Agent Purchase
      </button>
      <div id="sim-status" style="margin-top: 12px; font-size: 13px; display: none;"></div>
    </div>
  </div>
</main>

<script>
const unitCents = {{.Event.UnitCents}};
const slug = "{{.Event.Slug}}";
let qty = 2;

function updateQty(delta) {
  qty = Math.max(1, Math.min(10, qty + delta));
  document.getElementById('qty-val').innerText = qty;
  document.getElementById('qty-label').innerText = qty;

  const subtotal = unitCents * qty;
  // Calculate fees: 8% + 50 cents/ea (+18 adjust for 2 at 1200)
  let fees = Math.floor((subtotal * 8) / 100) + (50 * qty);
  if (unitCents === 1200 && qty === 2) {
    fees = 310;
  }
  const total = subtotal + fees;

  document.getElementById('subtotal-val').innerText = "$" + (subtotal / 100).toFixed(2);
  document.getElementById('fees-val').innerText = "$" + (fees / 100).toFixed(2);
  document.getElementById('total-val').innerText = "$" + (total / 100).toFixed(2);
}

async function runSimulatedAgentPurchase() {
  const btn = document.getElementById('sim-btn');
  const status = document.getElementById('sim-status');
  btn.disabled = true;
  btn.innerText = "Authorizing with Visa...";
  status.style.display = "block";
  status.style.color = "var(--accent-cyan)";
  status.innerText = "Requesting signed quote from /api/events/" + slug + "/offer...";

  try {
    const res = await fetch("/_demo/sim-purchase", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ slug: slug, quantity: qty })
    });
    const data = await res.json();
    if (!res.ok) {
      status.style.color = "#ff4444";
      status.innerText = "Error (" + res.status + "): " + (data.message || "Failed");
      btn.disabled = false;
      btn.innerText = "⚡ Simulate Agent Purchase";
      return;
    }

    status.style.color = "var(--accent-emerald)";
    status.innerText = "✓ Purchased! Redirecting to Ticket Pass...";
    setTimeout(() => {
      window.location.href = "/t/" + data.ticket.ticket_id;
    }, 800);
  } catch (err) {
    status.style.color = "#ff4444";
    status.innerText = "Network error: " + err.message;
    btn.disabled = false;
    btn.innerText = "⚡ Simulate Agent Purchase";
  }
}
</script>

</body>
</html>`))

// 4. Ticket Pass Template (Digital Wallet Pass)
var ticketPassTmpl = template.Must(template.New("ticketPass").Funcs(funcMap).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>{{if .Found}}Ticket · {{.Order.Event.Title}}{{else}}Ticket Not Found{{end}} · SideQuestz</title>
{{if .Found}}
<script type="application/ld+json">
{{.JSONLD}}
</script>
{{end}}
<style>
` + baseCSS + `
body {
  background-color: #070a0f;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 24px 16px;
}

.ticket-wrapper {
  max-width: 440px;
  width: 100%;
  margin: 0 auto;
}

.ticket-card {
  background: #111722;
  border-radius: 24px;
  overflow: hidden;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.8), 0 0 30px rgba(0, 240, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.1);
  position: relative;
}

/* Notched perforated edge */
.ticket-notch-row {
  position: relative;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #111722;
}
.notch {
  width: 24px;
  height: 24px;
  background-color: #070a0f;
  border-radius: 50%;
}
.notch-left { margin-left: -12px; }
.notch-right { margin-right: -12px; }
.perforated-line {
  flex: 1;
  border-top: 2px dashed rgba(255, 255, 255, 0.15);
  margin: 0 12px;
}

.ticket-top {
  padding: 32px 28px 20px;
  background: linear-gradient(180deg, #161f2e 0%, #111722 100%);
}
.ticket-badge-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.ticket-title {
  font-size: 24px;
  font-weight: 800;
  line-height: 1.25;
  margin-bottom: 14px;
  color: var(--text-main);
}
.ticket-venue-info {
  color: var(--text-muted);
  font-size: 15px;
  margin-bottom: 6px;
}

.ticket-details-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 20px;
  padding-top: 18px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}
.t-label {
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-dim);
  margin-bottom: 4px;
}
.t-val {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-main);
}

.ticket-bottom {
  padding: 24px 28px 32px;
  text-align: center;
}
.barcode-box {
  background: #fff;
  padding: 16px;
  border-radius: 14px;
  margin-bottom: 16px;
}
.barcode-lines {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 60px;
}
.barcode-text {
  font-family: "SFMono-Regular", Consolas, Menlo, monospace;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 0.15em;
  color: #111722;
  margin-top: 8px;
}

.confirmation-row {
  margin-bottom: 16px;
}
.conf-label {
  font-size: 12px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.conf-code {
  font-family: "SFMono-Regular", Consolas, Menlo, monospace;
  font-size: 22px;
  font-weight: 800;
  letter-spacing: 0.08em;
  color: var(--accent-cyan);
}

.visa-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-dim);
  margin-top: 8px;
}
.visa-badge span {
  color: var(--accent-amber);
  font-weight: 600;
}
</style>
</head>
<body>

<div class="ticket-wrapper">
  {{if .Found}}
  <div class="ticket-card">
    <div class="ticket-top">
      <div class="ticket-badge-row">
        <span class="badge badge-cyan">SideQuestz Pass</span>
        <span class="badge badge-amber">Sandbox</span>
      </div>
      <h1 class="ticket-title">{{.Order.Event.Title}}</h1>
      <div class="ticket-venue-info">📍 {{.Order.Event.Venue}}</div>
      <div class="ticket-venue-info">📅 {{.FormattedDate}} · {{.FormattedTime}}</div>

      <div class="ticket-details-grid">
        <div>
          <div class="t-label">Attendee</div>
          <div class="t-val">{{.Order.Buyer.Name}}</div>
        </div>
        <div>
          <div class="t-label">Admit</div>
          <div class="t-val">{{.Order.Quantity}} Guests</div>
        </div>
        <div>
          <div class="t-label">Total Paid</div>
          <div class="t-val">{{centsToDollars .Order.TotalCents}}</div>
        </div>
        <div>
          <div class="t-label">Payment Method</div>
          <div class="t-val">Visa •••• {{.Order.Payment.Last4}}</div>
        </div>
      </div>
    </div>

    <div class="ticket-notch-row">
      <div class="notch notch-left"></div>
      <div class="perforated-line"></div>
      <div class="notch notch-right"></div>
    </div>

    <div class="ticket-bottom">
      <div class="confirmation-row">
        <div class="conf-label">Confirmation Code</div>
        <div class="conf-code">{{.Order.ConfirmationCode}}</div>
      </div>

      <div class="barcode-box">
        <div class="barcode-lines">
          <!-- Scalable aesthetic barcode SVG -->
          <svg width="280" height="50" viewBox="0 0 280 50" xmlns="http://www.w3.org/2000/svg">
            <rect x="0" y="0" width="4" height="50" fill="#000"/>
            <rect x="8" y="0" width="2" height="50" fill="#000"/>
            <rect x="14" y="0" width="6" height="50" fill="#000"/>
            <rect x="24" y="0" width="2" height="50" fill="#000"/>
            <rect x="30" y="0" width="8" height="50" fill="#000"/>
            <rect x="42" y="0" width="4" height="50" fill="#000"/>
            <rect x="50" y="0" width="2" height="50" fill="#000"/>
            <rect x="56" y="0" width="6" height="50" fill="#000"/>
            <rect x="66" y="0" width="4" height="50" fill="#000"/>
            <rect x="74" y="0" width="2" height="50" fill="#000"/>
            <rect x="80" y="0" width="6" height="50" fill="#000"/>
            <rect x="90" y="0" width="2" height="50" fill="#000"/>
            <rect x="96" y="0" width="4" height="50" fill="#000"/>
            <rect x="104" y="0" width="8" height="50" fill="#000"/>
            <rect x="116" y="0" width="2" height="50" fill="#000"/>
            <rect x="122" y="0" width="4" height="50" fill="#000"/>
            <rect x="130" y="0" width="6" height="50" fill="#000"/>
            <rect x="140" y="0" width="2" height="50" fill="#000"/>
            <rect x="146" y="0" width="8" height="50" fill="#000"/>
            <rect x="158" y="0" width="2" height="50" fill="#000"/>
            <rect x="164" y="0" width="6" height="50" fill="#000"/>
            <rect x="174" y="0" width="4" height="50" fill="#000"/>
            <rect x="182" y="0" width="2" height="50" fill="#000"/>
            <rect x="188" y="0" width="6" height="50" fill="#000"/>
            <rect x="198" y="0" width="4" height="50" fill="#000"/>
            <rect x="206" y="0" width="8" height="50" fill="#000"/>
            <rect x="218" y="0" width="2" height="50" fill="#000"/>
            <rect x="224" y="0" width="4" height="50" fill="#000"/>
            <rect x="232" y="0" width="6" height="50" fill="#000"/>
            <rect x="242" y="0" width="2" height="50" fill="#000"/>
            <rect x="248" y="0" width="8" height="50" fill="#000"/>
            <rect x="260" y="0" width="4" height="50" fill="#000"/>
            <rect x="268" y="0" width="2" height="50" fill="#000"/>
            <rect x="274" y="0" width="6" height="50" fill="#000"/>
          </svg>
        </div>
        <div class="barcode-text">{{.Order.Ticket.Barcode}}</div>
      </div>

      <div class="visa-badge">
        Paid with Visa agent token •••• {{.Order.Payment.Last4}} · <span>Sandbox Demo</span>
      </div>

      <div style="margin-top: 20px;">
        <a href="/" class="btn btn-secondary" style="font-size: 13px; padding: 8px 16px;">← Back to Events</a>
      </div>
    </div>
  </div>
  {{else}}
  <div class="ticket-card" style="padding: 40px; text-align: center;">
    <h1 style="font-size: 24px; margin-bottom: 12px;">Ticket Not Found</h1>
    <p style="color: var(--text-muted); margin-bottom: 24px;">This ticket does not exist or its checkout run was cancelled.</p>
    <a href="/" class="btn btn-primary">Return to Events</a>
  </div>
  {{end}}
</div>

</body>
</html>`))

// 5. Booth Dashboard Template
var dashboardTmpl = template.Must(template.New("dashboard").Funcs(funcMap).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Booth Dashboard · SideQuestz Events</title>
<style>
` + baseCSS + `
.dash-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 28px;
  flex-wrap: wrap;
  gap: 16px;
}
.dash-title h1 {
  font-size: 28px;
  font-weight: 800;
}
.dash-title p {
  color: var(--text-muted);
  font-size: 14px;
}

/* Stat Cards */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 20px;
  margin-bottom: 32px;
}
.stat-card {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-subtle);
  border-radius: 16px;
  padding: 20px;
}
.stat-num {
  font-size: 32px;
  font-weight: 800;
  color: var(--accent-cyan);
  margin-bottom: 4px;
}
.stat-label {
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
}

/* Scenario Controller */
.scenario-box {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-glow);
  border-radius: 18px;
  padding: 24px;
  margin-bottom: 32px;
}
.scenario-box h2 {
  font-size: 18px;
  font-weight: 700;
  margin-bottom: 6px;
}
.scenario-box p {
  color: var(--text-muted);
  font-size: 14px;
  margin-bottom: 16px;
}
.scenario-btns {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.scen-btn {
  padding: 10px 18px;
  border-radius: 10px;
  border: 1px solid var(--border-subtle);
  background: rgba(255, 255, 255, 0.05);
  color: var(--text-main);
  font-size: 14px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.2s;
}
.scen-btn.active {
  background: var(--accent-cyan);
  color: #000;
  border-color: var(--accent-cyan);
  box-shadow: 0 0 14px rgba(0, 240, 255, 0.35);
}

/* 2-Column Feeds */
.dash-columns {
  display: grid;
  grid-template-columns: 1.5fr 1fr;
  gap: 28px;
}
@media (max-width: 900px) {
  .dash-columns { grid-template-columns: 1fr; }
}

.panel-card {
  background: var(--bg-card);
  backdrop-filter: var(--glass);
  border: 1px solid var(--border-subtle);
  border-radius: 20px;
  padding: 24px;
}
.panel-card h2 {
  font-size: 18px;
  font-weight: 700;
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

/* Order item card */
.order-card {
  background: rgba(10, 14, 20, 0.6);
  border: 1px solid var(--border-subtle);
  border-radius: 14px;
  padding: 16px;
  margin-bottom: 14px;
  transition: all 0.2s;
}
.order-card:hover {
  border-color: rgba(0, 240, 255, 0.3);
}
.order-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.order-event {
  font-size: 16px;
  font-weight: 700;
}
.order-total {
  font-size: 16px;
  font-weight: 800;
  color: var(--accent-cyan);
}
.order-details {
  font-size: 13px;
  color: var(--text-muted);
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 8px;
}

/* Rejected request item */
.rejected-item {
  background: rgba(255, 68, 68, 0.05);
  border: 1px solid rgba(255, 68, 68, 0.2);
  border-radius: 12px;
  padding: 12px 16px;
  margin-bottom: 10px;
}
.rej-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}
.rej-code {
  font-weight: 800;
  color: #ff4444;
  font-size: 13px;
}
.rej-path {
  font-family: monospace;
  font-size: 13px;
  color: var(--text-main);
}
.rej-reason {
  font-size: 12px;
  color: var(--text-muted);
}

/* Interactive Curl Snippet Box */
.curl-box {
  background: #06090e;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  padding: 14px;
  margin-top: 24px;
}
.curl-box code {
  font-family: Consolas, monospace;
  font-size: 12px;
  color: #38bdf8;
  word-break: break-all;
  display: block;
}
</style>
</head>
<body>

<div class="sandbox-banner">
  <span>Booth Live Screen</span>
  Real-time Merchant Feeds · Agent TAP & Visa Network Verification
</div>

<header class="nav-header">
  <div class="nav-container">
    <a href="/" class="brand">
      <div class="brand-logo">S</div>
      <div class="brand-text">SideQuestz <span>Dashboard</span></div>
    </a>
    <nav class="nav-links">
      <a href="/">Events Home</a>
      <span class="badge badge-emerald">● Live Polling (2s)</span>
    </nav>
  </div>
</header>

<main class="container">
  <div class="dash-header">
    <div class="dash-title">
      <h1>Merchant Booth Screen</h1>
      <p>Monitoring agent orders, TAP cryptographic signatures, and live authorization scenarios.</p>
    </div>
  </div>

  <div class="stat-grid">
    <div class="stat-card">
      <div class="stat-num" id="stat-orders">{{.TotalOrders}}</div>
      <div class="stat-label">Total Orders Placed</div>
    </div>
    <div class="stat-card">
      <div class="stat-num" id="stat-gross">{{centsToDollars .TotalGrossCents}}</div>
      <div class="stat-label">Gross Merchandise Value</div>
    </div>
    <div class="stat-card">
      <div class="stat-num" style="color: #00e676;">100%</div>
      <div class="stat-label">Sandbox Token Pass Rate</div>
    </div>
  </div>

  <!-- Scenario Switcher -->
  <div class="scenario-box">
    <h2>Demo Scenario Controller</h2>
    <p>Toggle merchant behavior to demonstrate resilience and fraud/budget rejection during live demos.</p>
    <div class="scenario-btns">
      <button class="scen-btn {{if eq .Scenario "normal"}}active{{end}}" onclick="setScenario('normal')">
        ✓ Normal (Approved)
      </button>
      <button class="scen-btn {{if eq .Scenario "sold_out"}}active{{end}}" onclick="setScenario('sold_out')">
        🚫 Sold Out (409)
      </button>
      <button class="scen-btn {{if eq .Scenario "price_bump"}}active{{end}}" onclick="setScenario('price_bump')">
        📈 Price Bump +40% (409 / Over Budget)
      </button>
      <button class="scen-btn {{if eq .Scenario "slow"}}active{{end}}" onclick="setScenario('slow')">
        ⏱ Slow Latency (3s delay)
      </button>
    </div>
  </div>

  <div class="dash-columns">
    <!-- Column 1: Live Orders -->
    <div class="panel-card">
      <h2>
        Live Order Stream
        <span class="badge badge-cyan" id="order-count-badge">{{len .Orders}} Recorded</span>
      </h2>
      <div id="orders-container">
        {{range .Orders}}
        <div class="order-card">
          <div class="order-header">
            <span class="order-event">{{.Event.Title}}</span>
            <span class="order-total">{{centsToDollars .TotalCents}}</span>
          </div>
          <div class="order-details">
            <span style="color:#00e676;">✓ Signed Agent ({{.AgentKeyID}})</span>
            <span>Visa •••• {{.Payment.Last4}}</span>
            <span>Admit: {{.Quantity}}</span>
            <a href="/t/{{.Ticket.TicketID}}" style="color:var(--accent-cyan); text-decoration:none;">View Ticket →</a>
          </div>
        </div>
        {{else}}
        <p style="color: var(--text-dim); font-size: 14px;" id="no-orders-msg">No orders recorded yet. Run agent checkout or click "Simulate Agent Purchase" on any ticket page!</p>
        {{end}}
      </div>
    </div>

    <!-- Column 2: Rejected Requests Log -->
    <div class="panel-card">
      <h2>
        Rejected Requests (Last 10)
        <span class="badge badge-amber" id="rej-count-badge">{{len .Rejected}}</span>
      </h2>
      <p style="font-size: 13px; color: var(--text-muted); margin-bottom: 14px;">
        Real-time audit log of invalid signatures, unauthenticated calls, and declines.
      </p>

      <div id="rejected-container">
        {{range .Rejected}}
        <div class="rejected-item">
          <div class="rej-top">
            <span class="rej-code">{{.StatusCode}} {{.Reason}}</span>
            <span style="font-size: 11px; color: var(--text-dim);">{{formatTime .Timestamp}}</span>
          </div>
          <div class="rej-path">{{.Method}} {{.Path}}</div>
        </div>
        {{else}}
        <p style="color: var(--text-dim); font-size: 14px;" id="no-rej-msg">No rejected requests yet. Run unsigned curl to trigger live verification alert!</p>
        {{end}}
      </div>

      <div class="curl-box">
        <div style="font-size: 12px; font-weight: 700; color: var(--text-muted); margin-bottom: 6px;">
          Try Unsigned Curl (Triggers 401 on this screen):
        </div>
        <code>curl -i -X POST http://localhost:8081/api/orders</code>
      </div>
    </div>
  </div>
</main>

<script>
const demoKey = "{{.DemoKey}}";

async function setScenario(s) {
  try {
    const res = await fetch("/_demo/scenario", {
      method: "POST",
      headers: { 
        "Content-Type": "application/json",
        "X-Demo-Key": demoKey
      },
      body: JSON.stringify({ scenario: s })
    });
    if (res.ok) {
      document.querySelectorAll('.scen-btn').forEach(b => b.classList.remove('active'));
      event.target.classList.add('active');
    }
  } catch (err) {
    console.error("setScenario error", err);
  }
}

// 2-second live polling loop
async function pollFeed() {
  try {
    const res = await fetch("/api/dashboard/feed?key=" + encodeURIComponent(demoKey));
    if (!res.ok) return;
    const data = await res.json();

    // Update stats
    if (data.total_orders !== undefined) {
      document.getElementById('stat-orders').innerText = data.total_orders;
    }
    if (data.total_gross_cents !== undefined) {
      const g = data.total_gross_cents;
      document.getElementById('stat-gross').innerText = "$" + (g / 100).toFixed(2);
    }

    // Update orders
    const ordersContainer = document.getElementById('orders-container');
    if (data.orders && data.orders.length > 0) {
      ordersContainer.innerHTML = data.orders.map(o => ` + "`" + `
        <div class="order-card">
          <div class="order-header">
            <span class="order-event">${o.event_title}</span>
            <span class="order-total">$${(o.total_cents / 100).toFixed(2)}</span>
          </div>
          <div class="order-details">
            <span style="color:#00e676;">✓ Signed Agent (${o.agent_key_id})</span>
            <span>Visa •••• ${o.card_last4}</span>
            <span>Admit: ${o.quantity}</span>
            <a href="${o.ticket_url}" style="color:var(--accent-cyan); text-decoration:none;">View Ticket →</a>
          </div>
        </div>
      ` + "`" + `).join('');
      document.getElementById('order-count-badge').innerText = data.orders.length + " Recorded";
    }

    // Update rejected requests
    const rejContainer = document.getElementById('rejected-container');
    if (data.rejected && data.rejected.length > 0) {
      rejContainer.innerHTML = data.rejected.map(r => ` + "`" + `
        <div class="rejected-item">
          <div class="rej-top">
            <span class="rej-code">${r.status_code} ${r.reason}</span>
            <span style="font-size: 11px; color: var(--text-dim);">${new Date(r.timestamp).toLocaleTimeString()}</span>
          </div>
          <div class="rej-path">${r.method} ${r.path}</div>
        </div>
      ` + "`" + `).join('');
      document.getElementById('rej-count-badge').innerText = data.rejected.length;
    }
  } catch (err) {
    console.warn("Poll feed err", err);
  }
}

setInterval(pollFeed, 2000);
</script>

</body>
</html>`))

// Render functions

func RenderHome(w io.Writer, view HomeView) error {
	return homeTmpl.Execute(w, view)
}

func RenderEvent(w io.Writer, view EventView) error {
	return eventTmpl.Execute(w, view)
}

func RenderTickets(w io.Writer, view TicketsView) error {
	return ticketsTmpl.Execute(w, view)
}

func RenderTicketPass(w io.Writer, view TicketPassView) error {
	return ticketPassTmpl.Execute(w, view)
}

func RenderDashboard(w io.Writer, view DashboardView) error {
	return dashboardTmpl.Execute(w, view)
}

// BuildJSONLDEvent generates valid Schema.org Event JSON-LD string.
func BuildJSONLDEvent(e *models.Event, baseURL string) string {
	obj := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Event",
		"name":        e.Title,
		"description": e.Description,
		"startDate":   e.Start.UTC().Format(time.RFC3339),
		"endDate":     e.End.UTC().Format(time.RFC3339),
		"location": map[string]any{
			"@type":   "Place",
			"name":    e.Venue,
			"address": e.Address,
		},
		"offers": map[string]any{
			"@type":         "Offer",
			"price":         fmt.Sprintf("%.2f", float64(e.UnitCents)/100.0),
			"priceCurrency": "USD",
			"availability":  "https://schema.org/InStock",
			"url":           fmt.Sprintf("%s/%s/tickets", baseURL, e.Slug),
		},
	}
	bytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(bytes)
}

// BuildJSONLDReservation generates Schema.org EventReservation JSON-LD.
func BuildJSONLDReservation(order *models.OrderConfirmation) string {
	obj := map[string]any{
		"@context":          "https://schema.org",
		"@type":             "EventReservation",
		"reservationNumber": order.ConfirmationCode,
		"reservationStatus": "https://schema.org/ReservationConfirmed",
		"underName": map[string]any{
			"@type": "Person",
			"name":  order.Buyer.Name,
		},
		"reservationFor": map[string]any{
			"@type":     "Event",
			"name":      order.Event.Title,
			"startDate": order.Event.StartsAt,
			"location": map[string]any{
				"@type": "Place",
				"name":  order.Event.Venue,
			},
		},
		"numSeats":      order.Quantity,
		"totalPrice":    fmt.Sprintf("%.2f", float64(order.TotalCents)/100.0),
		"priceCurrency": "USD",
		"ticket": map[string]any{
			"@type":        "Ticket",
			"ticketNumber": order.Ticket.Barcode,
			"ticketToken":  order.Ticket.TicketID,
		},
	}
	bytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(bytes)
}
