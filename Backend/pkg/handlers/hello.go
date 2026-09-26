package handlers

import (
	"Backend/pkg/datastore"
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"net/http"
	"strings"
	"time"
)

// HelloWorldHandler serves a rich interactive browser test page or JSON for API clients
func HelloWorldHandler(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	isJSONReq := strings.Contains(accept, "application/json") || r.URL.Query().Get("format") == "json"

	if isJSONReq {
		mongoStatus := "connected"
		if !datastore.IsConnected() {
			mongoStatus = "in_memory_fallback"
		}

		middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"message":         "Hello, World! SideQuestz API is fully operational 🚀",
			"status":          "ok",
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
			"database":        mongoStatus,
			"calendar_system": "RFC 5545 iCalendar (.ics) ready with public feed links",
			"version":         "1.0.0",
			"endpoints": map[string]string{
				"health":            "/hello",
				"calendar_link":     "/calendar/link",
				"calendar_export":   "/calendar/export.ics",
				"calendar_feed":     "/calendar/feed/{token}.ics",
				"calendar_days":     "/calendar/days",
				"activities":        "/activities",
				"activities_search": "/activities/search?q=atlanta",
				"events":            "/events",
				"places_search":     "/places/search?q=atlanta",
				"forum_posts":       "/forum/posts",
				"realtime_ws":       "/ws",
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(helloWorldHTML))
}

// HelloDemoICSTest returns a live sample public ICS feed token for the browser interactive tester
func HelloDemoICSTest(w http.ResponseWriter, r *http.Request) {
	demoEmail := "demo@sidequestz.app"
	u, err := store.GlobalStore.GetUserByEmail(demoEmail)
	if err != nil || u == nil {
		u, _ = store.GlobalStore.CreateUser(demoEmail, "hashed_demo_pw", "Demo Explorer")
	}
	demoUID := u.ID.Hex()
	token := store.GlobalStore.GetOrCreateCalendarToken(demoUID)

	// Ensure there is at least one sample itinerary for demo
	existingItins, _, _ := store.GlobalStore.ListActiveItineraries(demoUID, "", 10)
	if len(existingItins) == 0 {
		now := time.Now()
		tomorrow := now.AddDate(0, 0, 1)
		itin := &models.Itinerary{
			ID:           util.GenerateID(),
			HostUserID:   demoUID,
			HostName:     "Demo Explorer",
			Title:        "Midtown & Piedmont Park Quest",
			Date:         tomorrow.Format("2006-01-02"),
			StartTime:    "14:00",
			BackByTime:   "18:00",
			Status:       "active",
			Visibility:   "open",
			MaxGroupSize: 4,
			Members: []models.ItineraryMember{
				{
					UserID:      demoUID,
					Name:        "Demo Explorer",
					AvatarColor: "forest",
					Role:        "host",
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
			Items: []models.ItineraryItem{
				{
					ID:           util.GenerateID(),
					Title:        "Piedmont Park Picnic & Stroll",
					Type:         "outdoors",
					LocationName: "Piedmont Park",
					Address:      "1320 Monroe Dr NE, Atlanta, GA",
					ArriveTime:   tomorrow.Add(14 * time.Hour),
					DepartTime:   tomorrow.Add(16 * time.Hour),
					SharedNotes:  "Meet at the meadow near the lake",
				},
				{
					ID:           util.GenerateID(),
					Title:        "Ponce City Market Food Tour",
					Type:         "dining",
					LocationName: "Ponce City Market",
					Address:      "675 Ponce De Leon Ave NE, Atlanta, GA",
					ArriveTime:   tomorrow.Add(16*time.Hour + 30*time.Minute),
					DepartTime:   tomorrow.Add(18 * time.Hour),
					SharedNotes:  "Grabbing bites and rooftop view",
				},
			},
		}
		_ = store.GlobalStore.CreateItinerary(itin)
	}

	linkData := buildCalendarLinkResponse(r, token)
	icsFeed, _ := store.GlobalStore.GenerateUserICS(demoUID)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"token":       token,
		"calendar":    linkData,
		"raw_ics":     icsFeed,
		"events_info": "Includes active SideQuest itineraries and stops formatted in RFC 5545 standard",
	})
}

const helloWorldHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>SideQuestz — Hello World API Test</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;500;600;700;800&family=Plus+Jakarta+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg-primary: #0a0e17;
      --bg-secondary: #121826;
      --bg-card: rgba(22, 30, 49, 0.7);
      --bg-card-hover: rgba(30, 41, 67, 0.85);
      --border-color: rgba(255, 255, 255, 0.08);
      --border-accent: rgba(99, 102, 241, 0.35);
      --text-main: #f8fafc;
      --text-muted: #94a3b8;
      --text-sub: #64748b;
      --accent-primary: #6366f1;
      --accent-hover: #4f46e5;
      --accent-glow: rgba(99, 102, 241, 0.25);
      --emerald: #10b981;
      --emerald-glow: rgba(16, 185, 129, 0.25);
      --amber: #f59e0b;
      --cyan: #06b6d4;
    }

    * {
      margin: 0;
      padding: 0;
      box-sizing: border-box;
    }

    body {
      font-family: 'Plus Jakarta Sans', -apple-system, BlinkMacSystemFont, sans-serif;
      background: var(--bg-primary);
      color: var(--text-main);
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      position: relative;
      overflow-x: hidden;
      line-height: 1.5;
    }

    /* Ambient Background Glows */
    .ambient-glow-1 {
      position: absolute;
      top: -120px;
      left: 10%;
      width: 500px;
      height: 500px;
      background: radial-gradient(circle, rgba(99, 102, 241, 0.15) 0%, rgba(99, 102, 241, 0) 70%);
      pointer-events: none;
      z-index: 0;
    }
    .ambient-glow-2 {
      position: absolute;
      top: 200px;
      right: 5%;
      width: 450px;
      height: 450px;
      background: radial-gradient(circle, rgba(16, 185, 129, 0.12) 0%, rgba(16, 185, 129, 0) 70%);
      pointer-events: none;
      z-index: 0;
    }

    header {
      position: sticky;
      top: 0;
      z-index: 100;
      backdrop-filter: blur(20px);
      -webkit-backdrop-filter: blur(20px);
      background: rgba(10, 14, 23, 0.8);
      border-bottom: 1px solid var(--border-color);
      padding: 1rem 2rem;
    }

    .nav-container {
      max-width: 1200px;
      margin: 0 auto;
      display: flex;
      align-items: center;
      justify-content: space-between;
    }

    .logo-badge {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      text-decoration: none;
      color: inherit;
    }

    .logo-icon {
      width: 38px;
      height: 38px;
      background: linear-gradient(135deg, #6366f1, #a855f7);
      border-radius: 10px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 1.25rem;
      box-shadow: 0 4px 14px var(--accent-glow);
    }

    .logo-text {
      font-family: 'Outfit', sans-serif;
      font-weight: 700;
      font-size: 1.25rem;
      letter-spacing: -0.02em;
      background: linear-gradient(to right, #ffffff, #cbd5e1);
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
    }

    .nav-status {
      display: flex;
      align-items: center;
      gap: 1.5rem;
    }

    .status-pill {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      background: rgba(16, 185, 129, 0.1);
      border: 1px solid rgba(16, 185, 129, 0.25);
      color: #34d399;
      font-size: 0.825rem;
      font-weight: 600;
      padding: 0.35rem 0.85rem;
      border-radius: 9999px;
    }

    .status-dot {
      width: 8px;
      height: 8px;
      background: #10b981;
      border-radius: 50%;
      box-shadow: 0 0 10px #10b981;
      animation: pulse 2s infinite ease-in-out;
    }

    @keyframes pulse {
      0%, 100% { opacity: 1; transform: scale(1); }
      50% { opacity: 0.4; transform: scale(0.85); }
    }

    main {
      max-width: 1200px;
      margin: 2.5rem auto;
      padding: 0 1.5rem;
      flex: 1;
      position: relative;
      z-index: 10;
      width: 100%;
    }

    .hero-section {
      text-align: center;
      margin-bottom: 3.5rem;
    }

    .hero-badge {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      background: rgba(99, 102, 241, 0.12);
      border: 1px solid var(--border-accent);
      color: #a5b4fc;
      font-size: 0.875rem;
      font-weight: 600;
      padding: 0.4rem 1.1rem;
      border-radius: 9999px;
      margin-bottom: 1.25rem;
    }

    .hero-title {
      font-family: 'Outfit', sans-serif;
      font-size: clamp(2.5rem, 5vw, 4rem);
      font-weight: 800;
      letter-spacing: -0.03em;
      line-height: 1.1;
      margin-bottom: 1rem;
    }

    .gradient-text {
      background: linear-gradient(135deg, #818cf8 0%, #c084fc 50%, #38bdf8 100%);
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
    }

    .hero-desc {
      color: var(--text-muted);
      font-size: 1.125rem;
      max-width: 680px;
      margin: 0 auto;
    }

    /* Grid Layout */
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
      gap: 1.75rem;
      margin-bottom: 2.5rem;
    }

    .card {
      background: var(--bg-card);
      border: 1px solid var(--border-color);
      border-radius: 18px;
      padding: 1.75rem;
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
      transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);
      display: flex;
      flex-direction: column;
    }

    .card:hover {
      background: var(--bg-card-hover);
      border-color: var(--border-accent);
      transform: translateY(-3px);
      box-shadow: 0 12px 30px rgba(0, 0, 0, 0.35);
    }

    .card-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 1.25rem;
    }

    .card-title-group {
      display: flex;
      align-items: center;
      gap: 0.75rem;
    }

    .card-icon {
      width: 40px;
      height: 40px;
      border-radius: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 1.25rem;
    }

    .icon-indigo { background: rgba(99, 102, 241, 0.15); color: #818cf8; border: 1px solid rgba(99, 102, 241, 0.3); }
    .icon-emerald { background: rgba(16, 185, 129, 0.15); color: #34d399; border: 1px solid rgba(16, 185, 129, 0.3); }
    .icon-cyan { background: rgba(6, 182, 212, 0.15); color: #22d3ee; border: 1px solid rgba(6, 182, 212, 0.3); }

    .card-title {
      font-family: 'Outfit', sans-serif;
      font-size: 1.25rem;
      font-weight: 700;
    }

    .card-desc {
      color: var(--text-muted);
      font-size: 0.925rem;
      margin-bottom: 1.5rem;
      flex: 1;
    }

    .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 0.5rem;
      padding: 0.75rem 1.25rem;
      font-size: 0.925rem;
      font-weight: 600;
      font-family: inherit;
      border-radius: 12px;
      cursor: pointer;
      transition: all 0.2s ease;
      text-decoration: none;
      border: none;
    }

    .btn-primary {
      background: linear-gradient(135deg, #6366f1, #4f46e5);
      color: #ffffff;
      box-shadow: 0 4px 14px var(--accent-glow);
    }
    .btn-primary:hover {
      background: linear-gradient(135deg, #4f46e5, #4338ca);
      transform: translateY(-1px);
      box-shadow: 0 6px 20px rgba(99, 102, 241, 0.4);
    }

    .btn-secondary {
      background: rgba(255, 255, 255, 0.06);
      color: var(--text-main);
      border: 1px solid var(--border-color);
    }
    .btn-secondary:hover {
      background: rgba(255, 255, 255, 0.1);
      border-color: rgba(255, 255, 255, 0.2);
    }

    .btn-success {
      background: linear-gradient(135deg, #10b981, #059669);
      color: #ffffff;
      box-shadow: 0 4px 14px var(--emerald-glow);
    }
    .btn-success:hover {
      background: linear-gradient(135deg, #059669, #047857);
      transform: translateY(-1px);
    }

    /* Live Output Box */
    .output-box {
      margin-top: 1.25rem;
      background: #090d16;
      border: 1px solid rgba(255, 255, 255, 0.06);
      border-radius: 12px;
      padding: 1rem;
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.825rem;
      color: #cbd5e1;
      max-height: 220px;
      overflow-y: auto;
      white-space: pre-wrap;
      word-break: break-all;
      position: relative;
    }

    .output-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      color: var(--text-sub);
      font-size: 0.75rem;
      margin-bottom: 0.5rem;
      padding-bottom: 0.5rem;
      border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    }

    .link-group {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
      margin-top: 1rem;
    }

    .link-item {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 0.65rem 0.85rem;
      background: rgba(255, 255, 255, 0.03);
      border: 1px solid var(--border-color);
      border-radius: 8px;
      font-size: 0.85rem;
      color: #cbd5e1;
      text-decoration: none;
      transition: all 0.2s ease;
    }
    .link-item:hover {
      background: rgba(255, 255, 255, 0.08);
      border-color: var(--border-accent);
      color: #ffffff;
    }

    .copy-chip {
      background: rgba(99, 102, 241, 0.15);
      color: #a5b4fc;
      border: 1px solid rgba(99, 102, 241, 0.3);
      padding: 0.2rem 0.5rem;
      border-radius: 6px;
      font-size: 0.725rem;
      font-weight: 600;
      cursor: pointer;
    }
    .copy-chip:hover {
      background: rgba(99, 102, 241, 0.3);
    }

    footer {
      border-top: 1px solid var(--border-color);
      padding: 1.75rem 2rem;
      text-align: center;
      color: var(--text-sub);
      font-size: 0.85rem;
    }
  </style>
</head>
<body>
  <div class="ambient-glow-1"></div>
  <div class="ambient-glow-2"></div>

  <header>
    <div class="nav-container">
      <a href="/hello" class="logo-badge">
        <div class="logo-icon">🗺️</div>
        <span class="logo-text">SideQuestz API</span>
      </a>
      <div class="nav-status">
        <div class="status-pill">
          <span class="status-dot"></span>
          <span>System Healthy</span>
        </div>
      </div>
    </div>
  </header>

  <main>
    <section class="hero-section">
      <div class="hero-badge">✨ Verification Test Suite</div>
      <h1 class="hero-title">Hello, <span class="gradient-text">World!</span></h1>
      <p class="hero-desc">The SideQuestz backend server is online and running. Use this live test suite to verify endpoints, test calendar .ics feeds, and check realtime connections.</p>
    </section>

    <div class="grid">
      <!-- 1. Hello World API Ping -->
      <div class="card">
        <div class="card-header">
          <div class="card-title-group">
            <div class="card-icon icon-indigo">⚡</div>
            <h2 class="card-title">Hello World Ping</h2>
          </div>
          <span style="font-size: 0.8rem; color: #a5b4fc; font-weight: 600;">GET /hello</span>
        </div>
        <p class="card-desc">Tests backend connectivity, server time sync, database status, and JSON payload delivery.</p>
        <button id="pingBtn" class="btn btn-primary" onclick="runPingTest()">🚀 Run Hello World Test</button>
        <div class="output-box" id="pingOutput" style="display: none;">
          <div class="output-header">
            <span id="pingStatus">Status: Ready</span>
            <span id="pingLatency">0ms</span>
          </div>
          <code id="pingBody">Click the button above to execute...</code>
        </div>
      </div>

      <!-- 2. Calendar ICS System -->
      <div class="card">
        <div class="card-header">
          <div class="card-title-group">
            <div class="card-icon icon-emerald">📅</div>
            <h2 class="card-title">Calendar .ICS Feed</h2>
          </div>
          <span style="font-size: 0.8rem; color: #6ee7b7; font-weight: 600;">RFC 5545</span>
        </div>
        <p class="card-desc">Tests the universal iCalendar subscription feed. Generates a public link usable by Apple Calendar, Google Calendar, and Outlook.</p>
        <div style="display: flex; gap: 0.5rem; flex-wrap: wrap;">
          <button id="calBtn" class="btn btn-success" onclick="runCalendarTest()">🗓️ Test .ICS Feed</button>
          <a id="subBtn" class="btn btn-secondary" style="display: none;" target="_blank">🔗 Open Feed</a>
        </div>
        <div class="output-box" id="calOutput" style="display: none;">
          <div class="output-header">
            <span>RFC 5545 Feed Output</span>
            <span class="copy-chip" onclick="copyCalURL()">Copy URL</span>
          </div>
          <code id="calBody">Generating feed...</code>
        </div>
      </div>

      <!-- 3. Realtime WebSocket Test -->
      <div class="card">
        <div class="card-header">
          <div class="card-title-group">
            <div class="card-icon icon-cyan">📡</div>
            <h2 class="card-title">Realtime WebSocket</h2>
          </div>
          <span style="font-size: 0.8rem; color: #67e8f9; font-weight: 600;">WS /ws</span>
        </div>
        <p class="card-desc">Tests live 2-way WebSocket connection for chat messages, live location updates, and group checkouts.</p>
        <button id="wsBtn" class="btn btn-secondary" onclick="runWebSocketTest()">🔌 Connect WebSocket</button>
        <div class="output-box" id="wsOutput" style="display: none;">
          <div class="output-header">
            <span id="wsState">Disconnected</span>
            <span id="wsFrames">0 frames</span>
          </div>
          <code id="wsBody">Idle...</code>
        </div>
      </div>
    </div>

    <!-- Quick Navigation Links -->
    <div class="card" style="margin-bottom: 2rem;">
      <div class="card-header">
        <h3 class="card-title">Quick API Explorers</h3>
        <span style="font-size: 0.8rem; color: var(--text-sub);">Direct Browser GET</span>
      </div>
      <div class="link-group">
        <a class="link-item" href="/events" target="_blank">
          <span>Explore Scraped Events Catalog (<code>/events</code>)</span>
          <span style="color: var(--accent-primary);">View JSON &rarr;</span>
        </a>
        <a class="link-item" href="/places/search?q=atlanta" target="_blank">
          <span>Search Places & Pins (<code>/places/search?q=atlanta</code>)</span>
          <span style="color: var(--accent-primary);">View JSON &rarr;</span>
        </a>
        <a class="link-item" href="/forum/posts" target="_blank">
          <span>SideQuestz Community Forum (<code>/forum/posts</code>)</span>
          <span style="color: var(--accent-primary);">View JSON &rarr;</span>
        </a>
      </div>
    </div>
  </main>

  <footer>
    <p>SideQuestz API Backend &bull; HackGT 13 Project &bull; RFC 5545 iCalendar Enabled</p>
  </footer>

  <script>
    let activeCalURL = '';

    async function runPingTest() {
      const btn = document.getElementById('pingBtn');
      const box = document.getElementById('pingOutput');
      const statusSpan = document.getElementById('pingStatus');
      const latencySpan = document.getElementById('pingLatency');
      const bodyCode = document.getElementById('pingBody');

      btn.disabled = true;
      btn.innerText = 'Testing...';
      box.style.display = 'block';
      statusSpan.innerText = 'Requesting /hello...';
      bodyCode.innerText = 'Loading...';

      const t0 = performance.now();
      try {
        const resp = await fetch('/hello', {
          headers: { 'Accept': 'application/json' }
        });
        const duration = Math.round(performance.now() - t0);
        const data = await resp.json();

        statusSpan.innerText = 'Status: ' + resp.status + ' OK';
        statusSpan.style.color = '#34d399';
        latencySpan.innerText = duration + 'ms';
        bodyCode.innerText = JSON.stringify(data, null, 2);
      } catch (err) {
        statusSpan.innerText = 'Failed';
        statusSpan.style.color = '#ef4444';
        bodyCode.innerText = 'Error: ' + err.message;
      } finally {
        btn.disabled = false;
        btn.innerText = '🚀 Run Hello World Test';
      }
    }

    async function runCalendarTest() {
      const btn = document.getElementById('calBtn');
      const subBtn = document.getElementById('subBtn');
      const box = document.getElementById('calOutput');
      const bodyCode = document.getElementById('calBody');

      btn.disabled = true;
      btn.innerText = 'Generating...';
      box.style.display = 'block';

      try {
        const resp = await fetch('/test/calendar-demo');
        const data = await resp.json();

        activeCalURL = data.calendar.url;
        subBtn.href = data.calendar.url;
        subBtn.style.display = 'inline-flex';

        bodyCode.innerText = 'Public Subscribe Link:\n' + data.calendar.url + '\n\n' +
                             'Webcal URL (1-click iOS/macOS):\n' + data.calendar.webcal_url + '\n\n' +
                             '--- RFC 5545 iCalendar Payload Preview ---\n' +
                             data.raw_ics;
      } catch (err) {
        bodyCode.innerText = 'Error: ' + err.message;
      } finally {
        btn.disabled = false;
        btn.innerText = '🗓️ Test .ICS Feed';
      }
    }

    function copyCalURL() {
      if (activeCalURL) {
        navigator.clipboard.writeText(activeCalURL).then(() => {
          alert('Calendar Feed URL copied to clipboard!\nYou can subscribe to this URL in Google Calendar or Apple Calendar.');
        });
      }
    }

    function runWebSocketTest() {
      const btn = document.getElementById('wsBtn');
      const box = document.getElementById('wsOutput');
      const stateSpan = document.getElementById('wsState');
      const framesSpan = document.getElementById('wsFrames');
      const bodyCode = document.getElementById('wsBody');

      box.style.display = 'block';
      stateSpan.innerText = 'Connecting...';
      stateSpan.style.color = '#f59e0b';

      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = proto + '//' + window.location.host + '/ws';
      bodyCode.innerText = 'Initiating WebSocket handshake to ' + wsUrl + '...';

      let frameCount = 0;
      const ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        stateSpan.innerText = 'Connected ⚡';
        stateSpan.style.color = '#34d399';
        bodyCode.innerText = '[WS OPEN] Successfully established WebSocket connection!\nListening for live realtime events...';
        ws.send(JSON.stringify({ type: 'ping', timestamp: new Date().toISOString() }));
      };

      ws.onmessage = (event) => {
        frameCount++;
        framesSpan.innerText = frameCount + ' frames';
        bodyCode.innerText += '\n[WS RECEIVED]: ' + event.data;
      };

      ws.onerror = (err) => {
        stateSpan.innerText = 'Error';
        stateSpan.style.color = '#ef4444';
      };

      ws.onclose = () => {
        stateSpan.innerText = 'Closed';
        stateSpan.style.color = '#94a3b8';
      };
    }
  </script>
</body>
</html>
`
