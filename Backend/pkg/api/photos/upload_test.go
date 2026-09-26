package photos

import (
	"Backend/pkg/httpx"
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var (
	testJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 60)...)
	testPNG  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{9}, 60)...)
)

// multipartRequest builds a POST with the given fields (name → bytes) in order.
func multipartRequest(t *testing.T, fields ...any) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i := 0; i < len(fields); i += 2 {
		part, err := mw.CreateFormFile(fields[i].(string), "photo.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(fields[i+1].([]byte)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/me/photo", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func statusOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var se *httpx.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want a StatusError, got %v", err)
	}
	return se.Status, se.Message
}

func TestReadUpload(t *testing.T) {
	up, err := ReadUpload(multipartRequest(t, "note", []byte("hello"), Field, testJPEG), 1024)
	if err != nil || up.ContentType != TypeJPEG || !bytes.Equal(up.Bytes, testJPEG) {
		t.Fatalf("jpeg after another field: %v %+v", err, up)
	}
	if up, err := ReadUpload(multipartRequest(t, Field, testPNG), 1024); err != nil || up.ContentType != TypePNG {
		t.Fatalf("png: %v", err)
	}
	// Exactly the limit is fine; one byte over is 413 with the limit in words.
	exact := append(append([]byte{}, testJPEG[:4]...), bytes.Repeat([]byte{1}, 1020)...)
	if _, err := ReadUpload(multipartRequest(t, Field, exact), 1024); err != nil {
		t.Fatalf("exactly at the limit: %v", err)
	}
	status, msg := statusOf(t, func() error {
		_, err := ReadUpload(multipartRequest(t, Field, append(exact, 1)), 1024)
		return err
	}())
	if status != http.StatusRequestEntityTooLarge || msg != "That photo is too big. Pick one under 1 KB." {
		t.Fatalf("over the limit: %d %q", status, msg)
	}

	cases := []struct {
		name string
		req  *http.Request
		msg  string
	}{
		{"gif", multipartRequest(t, Field, []byte("GIF89a......")), MsgNotImage},
		{"empty file", multipartRequest(t, Field, []byte{}), MsgNotImage},
		{"html pretending", multipartRequest(t, Field, []byte("<html><script>alert(1)</script>")), MsgNotImage},
		{"wrong field", multipartRequest(t, "avatar", testJPEG), MsgNoPhoto},
		{"no parts", multipartRequest(t), MsgNoPhoto},
		{"json body", func() *http.Request {
			req := httptest.NewRequest("POST", "/me/photo", strings.NewReader(`{"photo":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			return req
		}(), MsgNoPhoto},
	}
	for _, tc := range cases {
		_, err := ReadUpload(tc.req, 1024)
		if status, msg := statusOf(t, err); status != http.StatusBadRequest || msg != tc.msg {
			t.Errorf("%s: %d %q", tc.name, status, msg)
		}
	}

	// A body cut off by the router's limit is 413 too.
	req := multipartRequest(t, Field, bytes.Repeat([]byte{0xFF}, 4096))
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 512)
	if status, _ := statusOf(t, func() error { _, err := ReadUpload(req, 1<<20); return err }()); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit: %d", status)
	}
}

func TestSizeLabelAndImageType(t *testing.T) {
	for n, want := range map[int64]string{2 << 20: "2 MB", 3 << 19: "1.5 MB", 512 << 10: "512 KB", 100: "100 bytes"} {
		if got := sizeLabel(n); got != want {
			t.Errorf("sizeLabel(%d) = %q, want %q", n, got, want)
		}
	}
	if ImageType(testJPEG) != TypeJPEG || ImageType(testPNG) != TypePNG || ImageType([]byte{0xFF, 0xD8}) != "" || ImageType(nil) != "" {
		t.Error("ImageType")
	}
}
