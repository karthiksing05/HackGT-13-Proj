package router

import (
	"Backend/pkg/handlers"
	"Backend/pkg/middleware"
	"Backend/pkg/realtime"
	"net/http"

	"github.com/gorilla/mux"
)

func protect(fn http.HandlerFunc) http.Handler {
	return middleware.BearerAuth(fn)
}

// SetupRoutes registers all API endpoints from API_ENDPOINTS.md
func SetupRoutes(r *mux.Router) {
	// Global CORS middleware
	r.Use(middleware.CORS)

	// WebSocket live updates route: WS /ws
	wsHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		realtime.HandleWebSocket(realtime.GlobalHub, w, req)
	})
	r.Handle("/ws", wsHandler).Methods("GET")

	// Register all endpoints directly on the root / (for API subdomain)
	registerEndpoints(r)

	// Custom 404 handler
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		middleware.WriteError(w, http.StatusNotFound, "Resource or endpoint not found")
	})
}

func registerEndpoints(r *mux.Router) {
	// ---------------- HELLO WORLD BROWSER TEST ----------------
	r.HandleFunc("/hello", handlers.HelloWorldHandler).Methods("GET")
	r.HandleFunc("/test/calendar-demo", handlers.HelloDemoICSTest).Methods("GET")

	// ---------------- AUTH (Public) ----------------
	r.HandleFunc("/auth/signup", handlers.Signup).Methods("POST")
	r.HandleFunc("/auth/login", handlers.Login).Methods("POST")
	r.HandleFunc("/auth/refresh", handlers.Refresh).Methods("POST")
	r.HandleFunc("/auth/password/forgot", handlers.ForgotPassword).Methods("POST")
	r.HandleFunc("/auth/password/verify", handlers.VerifyPasswordCode).Methods("POST")
	r.HandleFunc("/auth/password/reset", handlers.ResetPassword).Methods("POST")
	r.HandleFunc("/auth/password/resend", handlers.ResendPasswordCode).Methods("POST")

	// Auth (Protected)
	r.Handle("/auth/logout", protect(handlers.Logout)).Methods("POST")

	// ---------------- ME / PROFILE (Protected) ----------------
	r.Handle("/me", protect(handlers.GetMe)).Methods("GET")
	r.Handle("/me", protect(handlers.PatchMe)).Methods("PATCH")
	r.Handle("/me/photo", protect(handlers.UploadPhoto)).Methods("POST")
	r.Handle("/me/photo", protect(handlers.DeletePhoto)).Methods("DELETE")
	r.Handle("/me/avatar", protect(handlers.PatchAvatar)).Methods("PATCH")
	r.Handle("/me/preferences", protect(handlers.GetPreferences)).Methods("GET")
	r.Handle("/me/preferences", protect(handlers.PutPreferences)).Methods("PUT")
	r.Handle("/me/taste-profile", protect(handlers.GetTasteProfile)).Methods("GET")
	r.Handle("/me/devices", protect(handlers.PostDeviceToken)).Methods("POST")

	// ---------------- INTEGRATIONS & PAYMENTS ----------------
	r.Handle("/integrations", protect(handlers.GetIntegrations)).Methods("GET")
	r.Handle("/integrations/{provider}/connect", protect(handlers.ConnectIntegration)).Methods("POST")
	r.HandleFunc("/integrations/{provider}/callback", handlers.IntegrationCallback).Methods("GET")
	r.Handle("/integrations/{provider}", protect(handlers.DeleteIntegration)).Methods("DELETE")

	r.Handle("/me/payment-methods", protect(handlers.GetPaymentMethods)).Methods("GET")
	r.Handle("/me/payment-methods", protect(handlers.AddPaymentMethod)).Methods("POST")
	r.Handle("/me/payment-methods/{id}", protect(handlers.DeletePaymentMethod)).Methods("DELETE")

	// ---------------- CALENDAR (RFC 5545 .ics Feed & Sync) ----------------
	r.Handle("/calendar/link", protect(handlers.GetCalendarLink)).Methods("GET")
	r.Handle("/calendar/link/regenerate", protect(handlers.RegenerateCalendarLink)).Methods("POST")
	r.Handle("/calendar/export.ics", protect(handlers.ExportUserICS)).Methods("GET")
	r.Handle("/calendar/days", protect(handlers.GetCalendarDays)).Methods("GET")
	r.HandleFunc("/calendar/feed/{token}", handlers.ServeICSFeed).Methods("GET")
	r.HandleFunc("/calendar/{token:[a-zA-Z0-9_-]{16,64}(?:\\.ics)?}", handlers.ServeICSFeed).Methods("GET")

	// ---------------- ACTIVITIES, PLACES & EVENTS CATALOG ----------------
	r.HandleFunc("/activities", handlers.ListActivities).Methods("GET")
	r.HandleFunc("/activities/recommendations", handlers.RecommendActivities).Methods("GET")
	r.HandleFunc("/activities/search", handlers.SearchActivities).Methods("GET")
	r.HandleFunc("/activities/{id}", handlers.GetActivityDetail).Methods("GET")
	r.HandleFunc("/places/search", handlers.SearchPlaces).Methods("GET")
	r.HandleFunc("/places/reverse", handlers.ReverseGeocode).Methods("GET")
	r.HandleFunc("/events", handlers.ListEvents).Methods("GET")
	r.HandleFunc("/events/recommendations", handlers.RecommendActivities).Methods("GET")
	r.HandleFunc("/events/{id}", handlers.GetEventDetail).Methods("GET")

	// ---------------- PLANNING (CREATE FLOW) ----------------
	r.Handle("/plans/generate", protect(handlers.GeneratePlans)).Methods("POST")
	r.Handle("/plans/generate/more", protect(handlers.GenerateMorePlans)).Methods("POST")
	r.Handle("/plans/route", protect(handlers.RoutePlan)).Methods("POST")
	r.Handle("/itineraries", protect(handlers.CreateItinerary)).Methods("POST")

	// ---------------- ITINERARIES (HOME) ----------------
	r.Handle("/itineraries", protect(handlers.ListItineraries)).Methods("GET")
	r.Handle("/itineraries/{id}", protect(handlers.GetItinerary)).Methods("GET")
	r.Handle("/itineraries/{id}", protect(handlers.PatchItinerary)).Methods("PATCH")
	r.Handle("/itineraries/{id}", protect(handlers.DeleteItinerary)).Methods("DELETE")
	r.Handle("/itineraries/{id}/items/{itemId}", protect(handlers.PatchItineraryItemNotes)).Methods("PATCH")
	r.Handle("/itineraries/{id}/items/{itemId}/transit", protect(handlers.GetItemTransit)).Methods("GET")

	// ---------------- PAST EVENTS & RATINGS ----------------
	r.Handle("/me/past-events", protect(handlers.GetPastEvents)).Methods("GET")
	r.Handle("/ratings/{itemId}", protect(handlers.PutRating)).Methods("PUT")

	// ---------------- AGENT CHECKOUT (VISA) ----------------
	r.Handle("/checkout/intents", protect(handlers.CreateCheckoutIntent)).Methods("POST")
	r.Handle("/checkout/intents/{id}", protect(handlers.GetCheckoutIntent)).Methods("GET")
	r.Handle("/checkout/intents/{id}/approve", protect(handlers.ApproveCheckoutIntent)).Methods("POST")
	r.Handle("/checkout/intents/{id}/cancel", protect(handlers.CancelCheckoutIntent)).Methods("POST")

	// ---------------- FORUM ----------------
	r.HandleFunc("/forum/posts", handlers.ListForumPosts).Methods("GET")
	r.Handle("/forum/posts", protect(handlers.CreateForumPost)).Methods("POST")
	r.Handle("/forum/posts/{id}", protect(handlers.DeleteForumPost)).Methods("DELETE")
	r.Handle("/forum/posts/{id}/join-requests", protect(handlers.CreateJoinRequest)).Methods("POST")
	r.Handle("/forum/posts/{id}/join-requests", protect(handlers.DeleteJoinRequest)).Methods("DELETE")
	r.Handle("/forum/posts/{id}/plan-together", protect(handlers.PlanTogether)).Methods("POST")
	r.Handle("/itineraries/{id}/join-requests", protect(handlers.GetItineraryJoinRequests)).Methods("GET")
	r.Handle("/join-requests/{id}/approve", protect(handlers.ApproveJoinRequest)).Methods("POST")
	r.Handle("/join-requests/{id}/decline", protect(handlers.DeclineJoinRequest)).Methods("POST")

	// ---------------- THREADS (GROUP CHATS + DMS) ----------------
	r.Handle("/threads", protect(handlers.ListThreads)).Methods("GET")
	r.Handle("/threads/{id}/messages", protect(handlers.GetThreadMessages)).Methods("GET")
	r.Handle("/threads/{id}/messages", protect(handlers.SendMessage)).Methods("POST")
	r.Handle("/threads/dm", protect(handlers.StartDM)).Methods("POST")

	// ---------------- GROUP ALBUM ----------------
	r.Handle("/groups/{id}/photos", protect(handlers.ListGroupPhotos)).Methods("GET")
	r.Handle("/groups/{id}/photos", protect(handlers.UploadGroupPhoto)).Methods("POST")
	r.Handle("/groups/{id}/photos/{photoId}", protect(handlers.DeleteGroupPhoto)).Methods("DELETE")

	// ---------------- GROUP SPLITS (EQUAL) ----------------
	r.Handle("/groups/{id}/expenses", protect(handlers.ListExpenses)).Methods("GET")
	r.Handle("/groups/{id}/expenses", protect(handlers.CreateExpense)).Methods("POST")
	r.Handle("/groups/{id}/expenses/{expenseId}", protect(handlers.DeleteExpense)).Methods("DELETE")
	r.Handle("/groups/{id}/balances", protect(handlers.GetGroupBalances)).Methods("GET")
	r.Handle("/groups/{id}/settle", protect(handlers.SettleGroupBalance)).Methods("POST")

	// ---------------- FRIENDS ----------------
	r.Handle("/friends", protect(handlers.ListFriends)).Methods("GET")
	r.Handle("/users/search", protect(handlers.SearchUsers)).Methods("GET")
	r.Handle("/friends/requests", protect(handlers.ListFriendRequests)).Methods("GET")
	r.Handle("/friends/requests", protect(handlers.SendFriendRequest)).Methods("POST")
	r.Handle("/friends/requests/{id}/accept", protect(handlers.AcceptFriendRequest)).Methods("POST")
	r.Handle("/friends/requests/{id}/decline", protect(handlers.DeclineFriendRequest)).Methods("POST")
	r.Handle("/friends/{id}", protect(handlers.RemoveFriend)).Methods("DELETE")
	r.Handle("/invites", protect(handlers.CreateInvite)).Methods("POST")
}
