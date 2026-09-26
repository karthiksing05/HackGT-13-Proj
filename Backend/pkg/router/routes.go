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

	// WebSocket live updates route: WS /ws and /api/ws
	wsHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		realtime.HandleWebSocket(realtime.GlobalHub, w, req)
	})
	r.Handle("/ws", wsHandler).Methods("GET")
	r.Handle("/api/ws", wsHandler).Methods("GET")

	// Register on root and /api prefixes
	registerEndpoints(r, "")
	registerEndpoints(r, "/api")

	// Custom 404 handler
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		middleware.WriteError(w, http.StatusNotFound, "Resource or endpoint not found")
	})
}

func registerEndpoints(r *mux.Router, prefix string) {
	p := func(path string) string {
		return prefix + path
	}

	// ---------------- AUTH (Public) ----------------
	r.HandleFunc(p("/auth/signup"), handlers.Signup).Methods("POST")
	r.HandleFunc(p("/auth/login"), handlers.Login).Methods("POST")
	r.HandleFunc(p("/auth/refresh"), handlers.Refresh).Methods("POST")
	r.HandleFunc(p("/auth/password/forgot"), handlers.ForgotPassword).Methods("POST")
	r.HandleFunc(p("/auth/password/verify"), handlers.VerifyPasswordCode).Methods("POST")
	r.HandleFunc(p("/auth/password/reset"), handlers.ResetPassword).Methods("POST")
	r.HandleFunc(p("/auth/password/resend"), handlers.ResendPasswordCode).Methods("POST")

	// Auth (Protected)
	r.Handle(p("/auth/logout"), protect(handlers.Logout)).Methods("POST")

	// ---------------- ME / PROFILE (Protected) ----------------
	r.Handle(p("/me"), protect(handlers.GetMe)).Methods("GET")
	r.Handle(p("/me"), protect(handlers.PatchMe)).Methods("PATCH")
	r.Handle(p("/me/photo"), protect(handlers.UploadPhoto)).Methods("POST")
	r.Handle(p("/me/photo"), protect(handlers.DeletePhoto)).Methods("DELETE")
	r.Handle(p("/me/avatar"), protect(handlers.PatchAvatar)).Methods("PATCH")
	r.Handle(p("/me/preferences"), protect(handlers.GetPreferences)).Methods("GET")
	r.Handle(p("/me/preferences"), protect(handlers.PutPreferences)).Methods("PUT")
	r.Handle(p("/me/taste-profile"), protect(handlers.GetTasteProfile)).Methods("GET")
	r.Handle(p("/me/devices"), protect(handlers.PostDeviceToken)).Methods("POST")

	// ---------------- INTEGRATIONS & PAYMENTS ----------------
	r.Handle(p("/integrations"), protect(handlers.GetIntegrations)).Methods("GET")
	r.Handle(p("/integrations/{provider}/connect"), protect(handlers.ConnectIntegration)).Methods("POST")
	r.HandleFunc(p("/integrations/{provider}/callback"), handlers.IntegrationCallback).Methods("GET")
	r.Handle(p("/integrations/{provider}"), protect(handlers.DeleteIntegration)).Methods("DELETE")

	r.Handle(p("/me/payment-methods"), protect(handlers.GetPaymentMethods)).Methods("GET")
	r.Handle(p("/me/payment-methods"), protect(handlers.AddPaymentMethod)).Methods("POST")
	r.Handle(p("/me/payment-methods/{id}"), protect(handlers.DeletePaymentMethod)).Methods("DELETE")

	// ---------------- CALENDAR ----------------
	r.Handle(p("/calendar/days"), protect(handlers.GetCalendarDays)).Methods("GET")

	// ---------------- PLACES & EVENTS CATALOG ----------------
	r.HandleFunc(p("/places/search"), handlers.SearchPlaces).Methods("GET")
	r.HandleFunc(p("/places/reverse"), handlers.ReverseGeocode).Methods("GET")
	r.HandleFunc(p("/events"), handlers.ListEvents).Methods("GET")
	r.HandleFunc(p("/events/{id}"), handlers.GetEventDetail).Methods("GET")

	// ---------------- PLANNING (CREATE FLOW) ----------------
	r.Handle(p("/plans/generate"), protect(handlers.GeneratePlans)).Methods("POST")
	r.Handle(p("/plans/generate/more"), protect(handlers.GenerateMorePlans)).Methods("POST")
	r.Handle(p("/plans/route"), protect(handlers.RoutePlan)).Methods("POST")
	r.Handle(p("/itineraries"), protect(handlers.CreateItinerary)).Methods("POST")

	// ---------------- ITINERARIES (HOME) ----------------
	r.Handle(p("/itineraries"), protect(handlers.ListItineraries)).Methods("GET")
	r.Handle(p("/itineraries/{id}"), protect(handlers.GetItinerary)).Methods("GET")
	r.Handle(p("/itineraries/{id}"), protect(handlers.PatchItinerary)).Methods("PATCH")
	r.Handle(p("/itineraries/{id}"), protect(handlers.DeleteItinerary)).Methods("DELETE")
	r.Handle(p("/itineraries/{id}/items/{itemId}"), protect(handlers.PatchItineraryItemNotes)).Methods("PATCH")
	r.Handle(p("/itineraries/{id}/items/{itemId}/transit"), protect(handlers.GetItemTransit)).Methods("GET")

	// ---------------- PAST EVENTS & RATINGS ----------------
	r.Handle(p("/me/past-events"), protect(handlers.GetPastEvents)).Methods("GET")
	r.Handle(p("/ratings/{itemId}"), protect(handlers.PutRating)).Methods("PUT")

	// ---------------- AGENT CHECKOUT (VISA) ----------------
	r.Handle(p("/checkout/intents"), protect(handlers.CreateCheckoutIntent)).Methods("POST")
	r.Handle(p("/checkout/intents/{id}"), protect(handlers.GetCheckoutIntent)).Methods("GET")
	r.Handle(p("/checkout/intents/{id}/approve"), protect(handlers.ApproveCheckoutIntent)).Methods("POST")
	r.Handle(p("/checkout/intents/{id}/cancel"), protect(handlers.CancelCheckoutIntent)).Methods("POST")

	// ---------------- FORUM ----------------
	r.HandleFunc(p("/forum/posts"), handlers.ListForumPosts).Methods("GET")
	r.Handle(p("/forum/posts"), protect(handlers.CreateForumPost)).Methods("POST")
	r.Handle(p("/forum/posts/{id}"), protect(handlers.DeleteForumPost)).Methods("DELETE")
	r.Handle(p("/forum/posts/{id}/join-requests"), protect(handlers.CreateJoinRequest)).Methods("POST")
	r.Handle(p("/forum/posts/{id}/join-requests"), protect(handlers.DeleteJoinRequest)).Methods("DELETE")
	r.Handle(p("/forum/posts/{id}/plan-together"), protect(handlers.PlanTogether)).Methods("POST")
	r.Handle(p("/itineraries/{id}/join-requests"), protect(handlers.GetItineraryJoinRequests)).Methods("GET")
	r.Handle(p("/join-requests/{id}/approve"), protect(handlers.ApproveJoinRequest)).Methods("POST")
	r.Handle(p("/join-requests/{id}/decline"), protect(handlers.DeclineJoinRequest)).Methods("POST")

	// ---------------- THREADS (GROUP CHATS + DMS) ----------------
	r.Handle(p("/threads"), protect(handlers.ListThreads)).Methods("GET")
	r.Handle(p("/threads/{id}/messages"), protect(handlers.GetThreadMessages)).Methods("GET")
	r.Handle(p("/threads/{id}/messages"), protect(handlers.SendMessage)).Methods("POST")
	r.Handle(p("/threads/dm"), protect(handlers.StartDM)).Methods("POST")

	// ---------------- GROUP ALBUM ----------------
	r.Handle(p("/groups/{id}/photos"), protect(handlers.ListGroupPhotos)).Methods("GET")
	r.Handle(p("/groups/{id}/photos"), protect(handlers.UploadGroupPhoto)).Methods("POST")
	r.Handle(p("/groups/{id}/photos/{photoId}"), protect(handlers.DeleteGroupPhoto)).Methods("DELETE")

	// ---------------- GROUP SPLITS (EQUAL) ----------------
	r.Handle(p("/groups/{id}/expenses"), protect(handlers.ListExpenses)).Methods("GET")
	r.Handle(p("/groups/{id}/expenses"), protect(handlers.CreateExpense)).Methods("POST")
	r.Handle(p("/groups/{id}/expenses/{expenseId}"), protect(handlers.DeleteExpense)).Methods("DELETE")
	r.Handle(p("/groups/{id}/balances"), protect(handlers.GetGroupBalances)).Methods("GET")
	r.Handle(p("/groups/{id}/settle"), protect(handlers.SettleGroupBalance)).Methods("POST")

	// ---------------- FRIENDS ----------------
	r.Handle(p("/friends"), protect(handlers.ListFriends)).Methods("GET")
	r.Handle(p("/users/search"), protect(handlers.SearchUsers)).Methods("GET")
	r.Handle(p("/friends/requests"), protect(handlers.ListFriendRequests)).Methods("GET")
	r.Handle(p("/friends/requests"), protect(handlers.SendFriendRequest)).Methods("POST")
	r.Handle(p("/friends/requests/{id}/accept"), protect(handlers.AcceptFriendRequest)).Methods("POST")
	r.Handle(p("/friends/requests/{id}/decline"), protect(handlers.DeclineFriendRequest)).Methods("POST")
	r.Handle(p("/friends/{id}"), protect(handlers.RemoveFriend)).Methods("DELETE")
	r.Handle(p("/invites"), protect(handlers.CreateInvite)).Methods("POST")
}
