package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"
)

// Notes are short: shared ones live inside the itinerary document.
const (
	maxNoteRunes   = 2000
	MsgNoteTooLong = "Keep notes under 2,000 characters."
)

// routeItem resolves the item of /itineraries/{id}/items/{itemId} or
// /events/{id} for a member: the itinerary and the item's index in it.
// Anything the viewer is not on is ErrNotFound.
func (h *H) routeItem(r *http.Request, userID string) (*models.Itinerary, int, error) {
	vars := mux.Vars(r)
	itemID, nested := vars["itemId"]
	if !nested {
		return h.d.Store.Itineraries().FindItem(r.Context(), vars["id"], userID)
	}
	it, err := h.d.Store.Itineraries().ForMember(r.Context(), vars["id"], userID)
	if err != nil {
		return nil, -1, err
	}
	for i := range it.Items {
		if it.Items[i].ID == itemID {
			return it, i, nil
		}
	}
	return nil, -1, store.ErrNotFound
}

// Event is GET /events/{id} → ItineraryItem: any block of an itinerary the
// viewer is on, with their own notes, travel choice, rating and ticket.
func (h *H) Event(w http.ResponseWriter, r *http.Request) {
	uid := api.UserID(r)
	it, i, err := h.routeItem(r, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	v, err := newViewer(r.Context(), h.d, uid, []*models.Itinerary{it}, true)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v.item(it, &it.Items[i]))
}

// PatchNotes is PATCH /itineraries/{id}/items/{itemId} and PATCH
// /events/{id} ({notes, notes_scope?}) → 204. The note is the viewer's own;
// notes_scope omitted keeps the saved scope (private when none). A shared
// note is also written on the item for everyone on the plan, and switching
// your shared note to private takes it back; either way the other members
// hear itinerary.updated.
func (h *H) PatchNotes(w http.ResponseWriter, r *http.Request) {
	var req contract.ItemNotesPatch
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.NotesScope != nil && !req.NotesScope.Valid() {
		httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
		return
	}
	if utf8.RuneCountInString(req.Notes) > maxNoteRunes {
		httpx.Error(w, http.StatusBadRequest, MsgNoteTooLong)
		return
	}
	ctx, uid := r.Context(), api.UserID(r)
	it, i, err := h.routeItem(r, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	item := it.Items[i]
	scope := contract.NotesPrivate
	var savedScope *string
	if req.NotesScope != nil {
		scope = *req.NotesScope
		s := string(scope)
		savedScope = &s
	} else {
		state, err := h.d.Store.ItemStates().Get(ctx, uid, item.ID)
		switch {
		case err == nil && state.NotesScope != "":
			scope = contract.NotesScope(state.NotesScope)
		case err != nil && !errors.Is(err, store.ErrNotFound):
			api.Fail(w, r, err)
			return
		}
	}
	var notes *string
	if strings.TrimSpace(req.Notes) != "" {
		text := req.Notes
		notes = &text
	}
	if err := h.d.Store.ItemStates().SetNotes(ctx, uid, item.ID, it.ID, notes, savedScope); err != nil {
		api.Fail(w, r, err)
		return
	}
	var changed *models.Itinerary
	switch {
	case scope == contract.NotesShared:
		changed, err = h.d.Store.Itineraries().SetSharedNotes(ctx, it.ID, item.ID, notes, uid)
	case item.SharedNotes != nil && item.SharedNotesBy == uid:
		changed, err = h.d.Store.Itineraries().SetSharedNotes(ctx, it.ID, item.ID, nil, "")
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if changed != nil {
		PublishUpdated(ctx, h.d, changed, others(changed.MemberIDs, uid))
	}
	httpx.NoContent(w)
}
