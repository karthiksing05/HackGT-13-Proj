package me_test

import (
	"Backend/pkg/api/me"
	"Backend/pkg/api/photos"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

var (
	jpeg = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{5}, 300)...)
	png  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{6}, 300)...)
)

// upload POSTs data as the multipart field "photo" to /me/photo.
func upload(t *testing.T, srv *testutil.Server, sess *testutil.Session, data []byte) *testutil.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("photo", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Content-Type": mw.FormDataContentType()}
	if sess != nil {
		headers["Authorization"] = sess.Bearer()
	}
	return srv.DoRaw(t, "POST", "/me/photo", &body, headers)
}

func photoURL(t *testing.T, srv *testutil.Server, sess *testutil.Session) *string {
	t.Helper()
	var u contract.User
	srv.Do(t, "GET", "/me", nil, sess).Expect(t, http.StatusOK).JSON(t, &u)
	return u.PhotoURL
}

func TestPhotoUploadServeReplaceDelete(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Photo Person")
	b := srv.Signup(t, "Other Photo")

	var first contract.URLResponse
	upload(t, srv, a, jpeg).Expect(t, http.StatusOK).JSON(t, &first)
	if !strings.HasPrefix(first.URL, "http://api.test/photos/") {
		t.Fatalf("url: %s", first.URL)
	}
	if got := photoURL(t, srv, a); got == nil || *got != first.URL {
		t.Fatalf("GET /me photo_url = %v, want %s", got, first.URL)
	}
	firstPath := strings.TrimPrefix(first.URL, "http://api.test")
	res := srv.DoRaw(t, "GET", firstPath, nil, nil).Expect(t, http.StatusOK)
	if !bytes.Equal(res.Body, jpeg) || res.Header.Get("Content-Type") != photos.TypeJPEG {
		t.Fatalf("served photo: %s, %d bytes", res.Header.Get("Content-Type"), len(res.Body))
	}

	// A PNG replaces it; the old photo is deleted.
	var second contract.URLResponse
	upload(t, srv, a, png).Expect(t, http.StatusOK).JSON(t, &second)
	if second.URL == first.URL {
		t.Fatal("a new upload must get a new URL")
	}
	secondPath := strings.TrimPrefix(second.URL, "http://api.test")
	if ct := srv.DoRaw(t, "GET", secondPath, nil, nil).Expect(t, http.StatusOK).Header.Get("Content-Type"); ct != photos.TypePNG {
		t.Fatalf("png served as %s", ct)
	}
	srv.DoRaw(t, "GET", firstPath, nil, nil).Expect(t, http.StatusNotFound)
	if got := photoURL(t, srv, a); got == nil || *got != second.URL {
		t.Fatalf("photo_url after replace: %v", got)
	}

	// B deleting "their" photo never touches A's.
	srv.Do(t, "DELETE", "/me/photo", nil, b).Expect(t, http.StatusNoContent)
	srv.DoRaw(t, "GET", secondPath, nil, nil).Expect(t, http.StatusOK)
	if got := photoURL(t, srv, a); got == nil {
		t.Fatal("B's delete cleared A's photo")
	}

	srv.Do(t, "DELETE", "/me/photo", nil, a).Expect(t, http.StatusNoContent)
	if got := photoURL(t, srv, a); got != nil {
		t.Fatalf("photo_url after delete: %s", *got)
	}
	srv.DoRaw(t, "GET", secondPath, nil, nil).Expect(t, http.StatusNotFound)
	srv.Do(t, "DELETE", "/me/photo", nil, a).Expect(t, http.StatusNoContent)
	if n, err := srv.Store.Collection("photos").CountDocuments(context.Background(), map[string]any{"ownerId": a.UserID}); err != nil || n != 0 {
		t.Fatalf("photos left behind: %d %v", n, err)
	}

	if res := upload(t, srv, nil, jpeg); res.Status != http.StatusUnauthorized {
		t.Fatalf("upload without a token: %d", res.Status)
	}
}

func TestPhotoUploadLimitsAndTypes(t *testing.T) {
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) { c.MaxPhotoBytes = 1024 }))
	a := srv.Signup(t, "Limit Person")

	atLimit := append(append([]byte{}, jpeg[:4]...), bytes.Repeat([]byte{1}, 1020)...)
	upload(t, srv, a, atLimit).Expect(t, http.StatusOK)
	res := upload(t, srv, a, append(atLimit, 1))
	if res.Status != http.StatusRequestEntityTooLarge || res.Message() != "That photo is too big. Pick one under 1 KB." {
		t.Fatalf("over the limit: %d %s", res.Status, res.Body)
	}
	// Far over: refused after reading one byte past the limit (the router's
	// own body limit is covered in pkg/api/photos).
	if res := upload(t, srv, a, append(append([]byte{}, jpeg[:4]...), bytes.Repeat([]byte{1}, 200<<10)...)); res.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("far over the limit: %d %s", res.Status, res.Body)
	}
	if res := upload(t, srv, a, []byte("GIF89a not allowed")); res.Status != http.StatusBadRequest || res.Message() != photos.MsgNotImage {
		t.Fatalf("gif: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "POST", "/me/photo", map[string]string{"photo": "x"}, a); res.Status != http.StatusBadRequest || res.Message() != photos.MsgNoPhoto {
		t.Fatalf("json body: %d %s", res.Status, res.Body)
	}
	// Failed uploads left the good one in place.
	if got := photoURL(t, srv, a); got == nil {
		t.Fatal("a rejected upload cleared the photo")
	}
}

func TestAvatarColor(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Color Person")
	b := srv.Signup(t, "Plain Color")

	srv.Do(t, "PATCH", "/me/avatar", contract.AvatarPatch{Color: contract.AvatarSage}, a).Expect(t, http.StatusNoContent)
	var u contract.User
	srv.Do(t, "GET", "/me", nil, a).Expect(t, http.StatusOK).JSON(t, &u)
	if u.AvatarColor != contract.AvatarSage {
		t.Fatalf("avatar_color: %s", u.AvatarColor)
	}
	srv.Do(t, "GET", "/me", nil, b).Expect(t, http.StatusOK).JSON(t, &u)
	if u.AvatarColor != contract.AvatarInk {
		t.Fatalf("B's color changed: %s", u.AvatarColor)
	}
	for _, bad := range []string{`{"color":"purple"}`, `{}`} {
		if res := putJSON(t, srv, "PATCH", "/me/avatar", []byte(bad), a); res.Status != http.StatusBadRequest || res.Message() != me.MsgAvatarColor {
			t.Errorf("%s: %d %s", bad, res.Status, res.Body)
		}
	}
	if res := srv.Do(t, "PATCH", "/me/avatar", contract.AvatarPatch{Color: contract.AvatarClay}, nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.Status)
	}
}
