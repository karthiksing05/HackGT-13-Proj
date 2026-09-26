package me

import (
	"Backend/pkg/api"
	"Backend/pkg/api/photos"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"context"
	"net/http"

	"github.com/rs/zerolog/log"
)

// UploadPhoto is POST /me/photo (multipart "photo": JPEG or PNG by its bytes,
// at most MAX_PHOTO_BYTES, else 413) → {"url"}. The new photo replaces the
// avatar and the previous avatar photo is deleted.
func (h *H) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	upload, err := photos.ReadUpload(r, h.d.Cfg.MaxPhotoBytes)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	userID := user.ID.Hex()
	photo := &models.Photo{OwnerID: userID, Kind: models.PhotoKindAvatar, ContentType: upload.ContentType, Bytes: upload.Bytes}
	if err := h.d.Store.Photos().Put(r.Context(), photo); err != nil {
		api.Fail(w, r, err)
		return
	}
	previous, err := h.d.Store.Users().SwapPhoto(r.Context(), userID, &photo.ID)
	if err != nil {
		h.dropAvatarPhoto(r.Context(), userID, photo.ID)
		api.Fail(w, r, err)
		return
	}
	h.dropAvatarPhoto(r.Context(), userID, previous)
	httpx.JSON(w, http.StatusOK, contract.URLResponse{URL: photos.URL(h.d.Cfg.PublicBaseURL, photo.ID)})
}

// DeletePhoto is DELETE /me/photo → 204 (also when there was none).
func (h *H) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	userID := user.ID.Hex()
	previous, err := h.d.Store.Users().SwapPhoto(r.Context(), userID, nil)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	h.dropAvatarPhoto(r.Context(), userID, previous)
	httpx.NoContent(w)
}

// dropAvatarPhoto deletes an avatar photo nothing points at any more. It runs
// after the user document changed, so a failure only leaves an unused photo
// behind and never fails the request.
func (h *H) dropAvatarPhoto(ctx context.Context, userID, photoID string) {
	if err := h.d.Store.Users().DeleteAvatarPhoto(context.WithoutCancel(ctx), userID, photoID); err != nil {
		log.Warn().Err(err).Str("user", userID).Str("photo", photoID).Msg("delete replaced avatar photo")
	}
}
