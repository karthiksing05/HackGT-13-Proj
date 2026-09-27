package router

import (
	"events/pkg/api"
	"net/http"

	"github.com/gorilla/mux"
)

// New builds and returns the HTTP handler for the Events merchant server.
func New(deps *api.Deps) http.Handler {
	r := mux.NewRouter()

	// Global logging & CORS middleware
	r.Use(corsMiddleware)

	// Health and System routes
	r.HandleFunc("/healthz", deps.HandleHealthz).Methods(http.MethodGet)

	// TAP Key Directory
	r.HandleFunc("/sandbox/tap/keys/{keyid}", deps.HandleGetTapKey).Methods(http.MethodGet)

	// Simulated Visa Network Endpoint
	r.HandleFunc("/sandbox/visa/authorize", deps.VisaNet.HTTPHandler()).Methods(http.MethodPost)

	// Demo Booth Controls & Helpers
	r.HandleFunc("/_demo/scenario", deps.HandleSetScenario).Methods(http.MethodPost)
	r.HandleFunc("/_demo/sim-purchase", deps.HandleSimulatePurchase).Methods(http.MethodPost)

	// Dashboard Feed
	r.HandleFunc("/api/dashboard/feed", deps.HandleDashboardFeed).Methods(http.MethodGet)

	// Merchant API routes (TAP signed)
	r.HandleFunc("/api/events/{slug}/offer", deps.HandleGetOffer).Methods(http.MethodGet)
	r.HandleFunc("/api/orders", deps.HandlePostOrder).Methods(http.MethodPost)
	r.HandleFunc("/api/orders/{order_id}", deps.HandleGetOrder).Methods(http.MethodGet)

	// Web UI routes
	r.HandleFunc("/", deps.HandleHome).Methods(http.MethodGet)
	r.HandleFunc("/events", deps.HandleHome).Methods(http.MethodGet)
	r.HandleFunc("/dashboard", deps.HandleDashboard).Methods(http.MethodGet)
	r.HandleFunc("/t/{ticket_id}", deps.HandleTicketPass).Methods(http.MethodGet)

	// Dynamic slug routes (must be registered after static routes like /dashboard and /healthz)
	r.HandleFunc("/{slug}/tickets", deps.HandleTickets).Methods(http.MethodGet)
	r.HandleFunc("/{slug}", deps.HandleEvent).Methods(http.MethodGet)

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Signature-Input, Signature, Idempotency-Key, X-Demo-Key, X-Sandbox-Network-Key, Accept")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
