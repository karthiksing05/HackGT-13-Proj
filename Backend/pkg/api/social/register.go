// Package social is the forum, joins, threads, messages, groups (photos,
// expenses, balances, settle), friends, invites, presence and the realtime
// events they emit (backend-contract §4 C and §5).
//
// Plan posts are derived: an itinerary whose visibility is not just_me is a
// plan post with the itinerary's id; forum_posts holds only free-now posts.
package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/itineraries"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

type H struct{ d *api.Deps }

// Register mounts the §4 C routes. Literal paths (/forum/posts/mine,
// /threads/dm, /friends/requests) come before their {id} siblings. It also
// lends the itineraries area this area's thread and forum-post views.
func Register(r *mux.Router, d *api.Deps) {
	itineraries.UseSocial(itinerarySocial{})
	h := &H{d: d}
	r.Handle("/forum/posts", d.Protect(h.ListPosts)).Methods("GET")
	r.Handle("/forum/posts/mine", d.Protect(h.MyPost)).Methods("GET")
	r.Handle("/forum/posts", d.Protect(h.CreatePost)).Methods("POST")
	r.Handle("/forum/posts/{id}", d.Protect(h.DeletePost)).Methods("DELETE")
	r.Handle("/forum/posts/{id}/join-requests", d.Protect(h.Join)).Methods("POST")
	r.Handle("/forum/posts/{id}/join-requests", d.Protect(h.CancelJoin)).Methods("DELETE")
	r.Handle("/forum/posts/{id}/plan-together", d.Protect(h.PlanTogether)).Methods("POST")

	r.Handle("/threads", d.Protect(h.ListThreads)).Methods("GET")
	r.Handle("/threads/dm", d.Protect(h.StartDM)).Methods("POST")
	r.Handle("/threads/{id}", d.Protect(h.GetThread)).Methods("GET")
	r.Handle("/threads/{id}/messages", d.Protect(h.ListMessages)).Methods("GET")
	r.Handle("/threads/{id}/messages", d.Protect(h.SendMessage)).Methods("POST")
	r.Handle("/threads/{id}/read", d.Protect(h.MarkRead)).Methods("POST")

	r.Handle("/groups/{id}/photos", d.Protect(h.ListPhotos)).Methods("GET")
	r.Handle("/groups/{id}/photos", d.Protect(h.UploadPhoto)).Methods("POST")
	r.Handle("/groups/{id}/photos/{photoId}", d.Protect(h.DeletePhoto)).Methods("DELETE")
	r.Handle("/groups/{id}/expenses", d.Protect(h.ListExpenses)).Methods("GET")
	r.Handle("/groups/{id}/expenses", d.Protect(h.AddExpense)).Methods("POST")
	r.Handle("/groups/{id}/expenses/{expenseId}", d.Protect(h.DeleteExpense)).Methods("DELETE")
	r.Handle("/groups/{id}/balances", d.Protect(h.Balances)).Methods("GET")
	r.Handle("/groups/{id}/settle", d.Protect(h.Settle)).Methods("POST")

	r.Handle("/friends", d.Protect(h.ListFriends)).Methods("GET")
	r.Handle("/users/search", d.Protect(h.SearchUsers)).Methods("GET")
	r.Handle("/people/suggested", d.Protect(h.SuggestPeople)).Methods("GET")
	r.Handle("/friends/requests", d.Protect(h.ListRequests)).Methods("GET")
	r.Handle("/friends/requests", d.Protect(h.SendRequest)).Methods("POST")
	r.Handle("/friends/requests/{id}", d.Protect(h.CancelRequest)).Methods("DELETE")
	r.Handle("/friends/requests/{id}/accept", d.Protect(h.AcceptRequest)).Methods("POST")
	r.Handle("/friends/requests/{id}/decline", d.Protect(h.DeclineRequest)).Methods("POST")
	r.Handle("/friends/{user_id}", d.Protect(h.RemoveFriend)).Methods("DELETE")
	r.Handle("/invites", d.Protect(h.CreateInvite)).Methods("POST")
	r.Handle("/invites/{code}/accept", d.Protect(h.AcceptInvite)).Methods("POST")
}

// logEventError notes a realtime payload that could not be rendered; the
// write it reports on already succeeded, so the request still does.
func logEventError(what string, err error) {
	if err != nil {
		log.Warn().Err(err).Str("event", what).Msg("social: event not sent")
	}
}
