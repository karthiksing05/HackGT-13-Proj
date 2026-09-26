package me

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// MsgAvatarColor is the sentence for an unknown avatar color.
const MsgAvatarColor = "Pick one of the five avatar colors."

// SetAvatarColor is PATCH /me/avatar {color} (ink, sage, clay, forest, sand) → 204.
func (h *H) SetAvatarColor(w http.ResponseWriter, r *http.Request) {
	var req contract.AvatarPatch
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !req.Color.Valid() {
		httpx.Error(w, http.StatusBadRequest, MsgAvatarColor)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if _, err := h.d.Store.Users().Update(r.Context(), user.ID.Hex(), bson.M{"avatarColor": string(req.Color)}); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}
