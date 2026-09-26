// Package social is the forum, joins, threads, messages, groups (photos,
// expenses, balances, settle), friends and invites (backend-C). Every route
// is a 501 until that agent lands.
package social

import (
	"Backend/pkg/api"

	"github.com/gorilla/mux"
)

// Register mounts the §4 C routes. Literal paths (/forum/posts/mine,
// /threads/dm, /friends/requests) come before their {id} siblings.
func Register(r *mux.Router, d *api.Deps) {
	api.Stub(r, d, "GET", "/forum/posts", true)
	api.Stub(r, d, "GET", "/forum/posts/mine", true)
	api.Stub(r, d, "POST", "/forum/posts", true)
	api.Stub(r, d, "DELETE", "/forum/posts/{id}", true)
	api.Stub(r, d, "POST", "/forum/posts/{id}/join-requests", true)
	api.Stub(r, d, "DELETE", "/forum/posts/{id}/join-requests", true)
	api.Stub(r, d, "POST", "/forum/posts/{id}/plan-together", true)

	api.Stub(r, d, "GET", "/threads", true)
	api.Stub(r, d, "POST", "/threads/dm", true)
	api.Stub(r, d, "GET", "/threads/{id}", true)
	api.Stub(r, d, "GET", "/threads/{id}/messages", true)
	api.Stub(r, d, "POST", "/threads/{id}/messages", true)
	api.Stub(r, d, "POST", "/threads/{id}/read", true)

	api.Stub(r, d, "GET", "/groups/{id}/photos", true)
	api.Stub(r, d, "POST", "/groups/{id}/photos", true)
	api.Stub(r, d, "DELETE", "/groups/{id}/photos/{photoId}", true)
	api.Stub(r, d, "GET", "/groups/{id}/expenses", true)
	api.Stub(r, d, "POST", "/groups/{id}/expenses", true)
	api.Stub(r, d, "DELETE", "/groups/{id}/expenses/{expenseId}", true)
	api.Stub(r, d, "GET", "/groups/{id}/balances", true)
	api.Stub(r, d, "POST", "/groups/{id}/settle", true)

	api.Stub(r, d, "GET", "/friends", true)
	api.Stub(r, d, "GET", "/users/search", true)
	api.Stub(r, d, "GET", "/friends/requests", true)
	api.Stub(r, d, "POST", "/friends/requests", true)
	api.Stub(r, d, "DELETE", "/friends/requests/{id}", true)
	api.Stub(r, d, "POST", "/friends/requests/{id}/accept", true)
	api.Stub(r, d, "POST", "/friends/requests/{id}/decline", true)
	api.Stub(r, d, "DELETE", "/friends/{user_id}", true)
	api.Stub(r, d, "POST", "/invites", true)
	api.Stub(r, d, "POST", "/invites/{code}/accept", true)
}
