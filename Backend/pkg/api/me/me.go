// Package me is the signed-in user's own profile (GET/PATCH /me here;
// preferences, taste profile, devices, photo and avatar in their own files).
package me

import (
	"Backend/pkg/api"
	socialapi "Backend/pkg/api/social"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/util"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Sentences shared with sign-up.
const (
	MsgName     = "Enter your name (at least 2 characters)."
	MsgUsername = "Usernames are 3–30 characters: letters, numbers, dots or underscores."
	MsgUnder13  = "You need to be 13 or older to use SideQuests."
	MsgStatus   = "Pick Open to all, Friends only or Busy."
)

type H struct{ d *api.Deps }

// Register mounts /me; the routes past GET/PATCH /me live in photo.go,
// avatar.go, preferences.go, taste.go and devices.go.
func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	r.Handle("/me", d.Protect(h.Get)).Methods("GET")
	r.Handle("/me", d.Protect(h.Patch)).Methods("PATCH")

	r.Handle("/me/photo", d.Protect(h.UploadPhoto)).Methods("POST")
	r.Handle("/me/photo", d.Protect(h.DeletePhoto)).Methods("DELETE")
	r.Handle("/me/avatar", d.Protect(h.SetAvatarColor)).Methods("PATCH")
	r.Handle("/me/preferences", d.Protect(h.GetPreferences)).Methods("GET")
	r.Handle("/me/preferences", d.Protect(h.PutPreferences)).Methods("PUT")
	r.Handle("/me/taste-profile", d.Protect(h.TasteProfile)).Methods("GET")
	r.Handle("/me/devices", d.Protect(h.RegisterDevice)).Methods("POST")
	r.Handle("/me/devices/{token}", d.Protect(h.UnregisterDevice)).Methods("DELETE")
}

// Get is GET /me → User.
func (h *H) Get(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view.User(user, h.d.Cfg.PublicBaseURL, h.d.Clock()))
}

// Patch is PATCH /me (UserPatch) → User. Only present fields change.
func (h *H) Patch(w http.ResponseWriter, r *http.Request) {
	var patch contract.UserPatch
	if !httpx.Decode(w, r, &patch) {
		return
	}
	now := h.d.Clock()
	set := bson.M{}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if !util.IsValidName(name) {
			httpx.Error(w, http.StatusBadRequest, MsgName)
			return
		}
		set["name"] = name
	}
	if patch.Username != nil {
		handle, ok := util.NormalizeUsername(*patch.Username)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, MsgUsername)
			return
		}
		set["username"] = handle
	}
	if patch.DateOfBirth != nil {
		if view.Age(patch.DateOfBirth.Time, now) < 13 {
			httpx.Error(w, http.StatusBadRequest, MsgUnder13)
			return
		}
		set["birthDate"] = patch.DateOfBirth.Time
	}
	if patch.Status != nil {
		if !patch.Status.Valid() {
			httpx.Error(w, http.StatusBadRequest, MsgStatus)
			return
		}
		set["status"] = string(*patch.Status)
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if len(set) > 0 {
		statusChanged := patch.Status != nil && string(*patch.Status) != user.Status
		user, err = h.d.Store.Users().Update(r.Context(), user.ID.Hex(), set)
		if err != nil {
			api.Fail(w, r, err)
			return
		}
		if statusChanged {
			// Seam (backend-C): friends hear the new status line.
			socialapi.StatusChanged(r.Context(), h.d, user, httpx.TZ(r))
		}
	}
	httpx.JSON(w, http.StatusOK, view.User(user, h.d.Cfg.PublicBaseURL, now))
}
