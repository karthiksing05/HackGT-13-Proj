package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/photos"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
)

// MsgPhotoOwner is the Album's answer to deleting someone else's photo.
const MsgPhotoOwner = "You can only delete photos you added."

// albumLimit caps GET /groups/{id}/photos.
const albumLimit = 200

// ListPhotos is GET /groups/{id}/photos → [GroupPhoto], newest first.
func (h *H) ListPhotos(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	list, err := h.d.Store.Photos().ListForGroup(r.Context(), th.ID, albumLimit)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	// Photos added in the same millisecond: the later (UUIDv7) id first.
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.After(list[j].CreatedAt)
		}
		return list[i].ID > list[j].ID
	})
	owners := make([]string, 0, len(list))
	for _, p := range list {
		owners = append(owners, p.OwnerID)
	}
	ppl, err := h.loadPeople(r.Context(), owners)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.GroupPhoto, 0, len(list))
	for _, p := range list {
		out = append(out, photoView(p, viewerID, ppl))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// UploadPhoto is POST /groups/{id}/photos (multipart "photo", JPEG or PNG,
// up to MAX_PHOTO_BYTES, read by photos.ReadUpload) → 201 GroupPhoto;
// members hear photo.added.
func (h *H) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	upload, err := photos.ReadUpload(r, h.d.Cfg.MaxPhotoBytes)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	photo := &models.Photo{OwnerID: viewerID, Kind: models.PhotoKindGroup, GroupID: th.ID, ContentType: upload.ContentType, Bytes: upload.Bytes}
	if err := h.d.Store.Photos().Put(r.Context(), photo); err != nil {
		api.Fail(w, r, err)
		return
	}
	photo.Bytes = nil
	ppl, err := h.loadPeople(r.Context(), th.MemberIDs)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	for _, id := range th.MemberIDs {
		realtime.PhotoAdded(h.d.Publish(), []string{id}, th.ID, photoView(photo, id, ppl))
	}
	h.pushThread(r.Context(), th, httpx.TZ(r), th.MemberIDs)
	httpx.JSON(w, http.StatusCreated, photoView(photo, viewerID, ppl))
}

// DeletePhoto is DELETE /groups/{id}/photos/{photoId}: only whoever added it
// (403 otherwise) → 204.
func (h *H) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	vars := mux.Vars(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), vars["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	photo, err := h.d.Store.Photos().GroupPhotoMeta(r.Context(), th.ID, vars["photoId"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if photo.OwnerID != viewerID {
		httpx.Error(w, http.StatusForbidden, MsgPhotoOwner)
		return
	}
	if err := h.d.Store.Photos().Delete(r.Context(), photo.ID); err != nil {
		api.Fail(w, r, err)
		return
	}
	h.pushThread(r.Context(), th, httpx.TZ(r), th.MemberIDs)
	httpx.NoContent(w)
}
