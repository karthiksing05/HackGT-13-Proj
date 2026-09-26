package photos_test

import (
	"Backend/pkg/api/photos"
	"Backend/pkg/models"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"net/http"
	"testing"
)

func TestServePhoto(t *testing.T) {
	srv := testutil.New(t)
	ctx := context.Background()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{3}, 200)...)
	photo := &models.Photo{OwnerID: "u1", Kind: models.PhotoKindGroup, GroupID: "g1", ContentType: photos.TypePNG, Bytes: png}
	if err := srv.Store.Photos().Put(ctx, photo); err != nil {
		t.Fatal(err)
	}
	path := "/photos/" + photo.ID

	// Public: no token needed.
	res := srv.DoRaw(t, "GET", path, nil, nil).Expect(t, http.StatusOK)
	if !bytes.Equal(res.Body, png) {
		t.Fatalf("body differs (%d bytes)", len(res.Body))
	}
	h := res.Header
	if h.Get("Content-Type") != "image/png" || h.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		h.Get("ETag") != `"`+photo.ID+`"` || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Last-Modified") == "" {
		t.Fatalf("headers: %v", h)
	}
	if got := photos.URL("http://api.test/", photo.ID); got != "http://api.test/photos/"+photo.ID {
		t.Fatalf("URL: %s", got)
	}

	// Revalidation is a bodiless 304.
	res = srv.DoRaw(t, "GET", path, nil, map[string]string{"If-None-Match": h.Get("ETag")}).Expect(t, http.StatusNotModified)
	if len(res.Body) != 0 {
		t.Fatalf("304 with a body: %q", res.Body)
	}
	// HEAD answers the headers only.
	res = srv.DoRaw(t, "HEAD", path, nil, nil).Expect(t, http.StatusOK)
	if len(res.Body) != 0 || res.Header.Get("Content-Length") != "208" {
		t.Fatalf("HEAD: %d bytes, length %s", len(res.Body), res.Header.Get("Content-Length"))
	}

	if res := srv.DoRaw(t, "GET", "/photos/nope", nil, nil); res.Status != http.StatusNotFound || res.Message() == "" {
		t.Fatalf("unknown photo: %d %s", res.Status, res.Body)
	}

	// Bytes stored with any other type never go out as something a browser renders.
	odd := &models.Photo{OwnerID: "u1", Kind: models.PhotoKindGroup, GroupID: "g1", ContentType: "text/html", Bytes: []byte("<script>alert(1)</script>")}
	if err := srv.Store.Photos().Put(ctx, odd); err != nil {
		t.Fatal(err)
	}
	if ct := srv.DoRaw(t, "GET", "/photos/"+odd.ID, nil, nil).Expect(t, http.StatusOK).Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("stored text/html served as %s", ct)
	}
}
