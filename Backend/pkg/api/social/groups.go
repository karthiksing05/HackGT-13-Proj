package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

// Sentences of the Album tab.
const (
	MsgPhotoMissing = "Pick a photo to upload."
	MsgPhotoType    = "That file isn't a photo. Pick a JPEG or PNG."
	MsgPhotoOwner   = "You can only delete photos you added."
)

// multipartHeadroom covers the form framing around the photo bytes.
const multipartHeadroom = 64 << 10

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
	photos, err := h.d.Store.Photos().ListForGroup(r.Context(), th.ID, albumLimit)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	owners := make([]string, 0, len(photos))
	for _, p := range photos {
		owners = append(owners, p.OwnerID)
	}
	ppl, err := h.loadPeople(r.Context(), owners)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.GroupPhoto, 0, len(photos))
	for _, p := range photos {
		out = append(out, photoView(p, viewerID, ppl))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// UploadPhoto is POST /groups/{id}/photos (multipart "photo", JPEG or PNG,
// up to MAX_PHOTO_BYTES) → 201 GroupPhoto; members hear photo.added.
func (h *H) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	data, contentType, err := h.readPhoto(w, r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	photo := &models.Photo{OwnerID: viewerID, Kind: models.PhotoKindGroup, GroupID: th.ID, ContentType: contentType, Bytes: data}
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

// readPhoto reads the multipart "photo" part: its bytes and a content type
// from the magic bytes (JPEG or PNG). Too large → 413.
func (h *H) readPhoto(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	limit := h.d.Cfg.MaxPhotoBytes
	tooBig := httpx.E(http.StatusRequestEntityTooLarge,
		fmt.Sprintf("That photo is too big. Pick one under %d MB.", max(1, limit>>20)))
	r.Body = http.MaxBytesReader(w, r.Body, limit+multipartHeadroom)
	if err := r.ParseMultipartForm(multipartHeadroom); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, "", tooBig
		}
		return nil, "", httpx.BadRequest(MsgPhotoMissing)
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, _, err := r.FormFile("photo")
	if err != nil {
		return nil, "", httpx.BadRequest(MsgPhotoMissing)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	switch {
	case err != nil:
		return nil, "", httpx.BadRequest(MsgPhotoMissing)
	case int64(len(data)) > limit:
		return nil, "", tooBig
	case len(data) == 0:
		return nil, "", httpx.BadRequest(MsgPhotoMissing)
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return data, "image/jpeg", nil
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return data, "image/png", nil
	}
	return nil, "", httpx.BadRequest(MsgPhotoType)
}
