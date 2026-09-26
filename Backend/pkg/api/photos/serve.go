package photos

import (
	"Backend/pkg/api"
	"bytes"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// URL is the public address of a stored photo.
func URL(publicBaseURL, id string) string {
	return strings.TrimRight(publicBaseURL, "/") + "/photos/" + id
}

// Serve is GET /photos/{id}: the stored bytes with a year-long immutable
// cache (a photo never changes under its id) and an ETag, so revalidation
// answers 304. Public: the id is an unguessable UUID.
func (h *H) Serve(w http.ResponseWriter, r *http.Request) {
	photo, err := h.d.Store.Photos().Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	contentType := photo.ContentType
	if contentType != TypeJPEG && contentType != TypePNG {
		contentType = "application/octet-stream" // never let a browser render stored bytes as a page
	}
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("ETag", `"`+photo.ID+`"`)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'")
	http.ServeContent(w, r, "", photo.CreatedAt, bytes.NewReader(photo.Bytes))
}
