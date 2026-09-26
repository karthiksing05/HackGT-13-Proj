package me

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
)

// Sentences for device registration.
const (
	MsgPushToken = "That push token doesn't look right."
	MsgPlatform  = "The platform must be ios or android."
)

// pushToken matches APNs (hex) and FCM (base64url with ":") tokens; it also
// keeps the token safe to use as the DELETE path segment.
var pushToken = regexp.MustCompile(`^[A-Za-z0-9:_-]{8,512}$`)

// RegisterDevice is POST /me/devices {push_token, platform} → 204. The token
// is stored for the account; nothing sends push yet.
func (h *H) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req contract.DeviceRegistration
	if !httpx.Decode(w, r, &req) {
		return
	}
	token := strings.TrimSpace(req.PushToken)
	if !pushToken.MatchString(token) {
		httpx.Error(w, http.StatusBadRequest, MsgPushToken)
		return
	}
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if platform == "" {
		platform = "ios"
	}
	if platform != "ios" && platform != "android" {
		httpx.Error(w, http.StatusBadRequest, MsgPlatform)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Devices().Upsert(r.Context(), user.ID.Hex(), token, platform); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// UnregisterDevice is DELETE /me/devices/{token} → 204; a token that is not
// registered to the caller is 404.
func (h *H) UnregisterDevice(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Devices().Delete(r.Context(), user.ID.Hex(), mux.Vars(r)["token"]); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}
