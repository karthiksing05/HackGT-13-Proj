package photos

import (
	"Backend/pkg/httpx"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Content types a stored photo can have.
const (
	TypeJPEG = "image/jpeg"
	TypePNG  = "image/png"
)

// Field is the multipart field every photo upload uses.
const Field = "photo"

// Sentences the app shows for a rejected upload.
const (
	MsgNoPhoto  = "Choose a photo to upload."
	MsgNotImage = "That file isn't a JPEG or PNG photo."
)

var (
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
)

// Upload is a photo read from a request, checked by its bytes.
type Upload struct {
	Bytes       []byte
	ContentType string // TypeJPEG or TypePNG
}

// ReadUpload reads the multipart field "photo" (POST /me/photo, POST
// /groups/{id}/photos). The type comes from the magic bytes, never from the
// client's headers: JPEG or PNG, else 400; more than maxBytes is 413. Errors
// are *httpx.StatusError, ready for api.Fail.
func ReadUpload(r *http.Request, maxBytes int64) (*Upload, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, httpx.BadRequest(MsgNoPhoto)
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, httpx.BadRequest(MsgNoPhoto)
		}
		if err != nil {
			return nil, readError(err, maxBytes)
		}
		if part.FormName() != Field {
			continue // NextPart skips the rest of this one
		}
		data, err := io.ReadAll(io.LimitReader(part, maxBytes+1))
		if err != nil {
			return nil, readError(err, maxBytes)
		}
		if int64(len(data)) > maxBytes {
			return nil, tooBig(maxBytes)
		}
		contentType := ImageType(data)
		if contentType == "" {
			return nil, httpx.BadRequest(MsgNotImage)
		}
		return &Upload{Bytes: data, ContentType: contentType}, nil
	}
}

// ImageType is TypeJPEG or TypePNG by the leading bytes, "" otherwise.
func ImageType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, jpegMagic):
		return TypeJPEG
	case bytes.HasPrefix(data, pngMagic):
		return TypePNG
	}
	return ""
}

// readError maps a failed read: the body limit is 413, a malformed body 400.
func readError(err error, maxBytes int64) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return tooBig(maxBytes)
	}
	return httpx.Wrap(http.StatusBadRequest, MsgNoPhoto, err)
}

func tooBig(maxBytes int64) error {
	return httpx.E(http.StatusRequestEntityTooLarge, fmt.Sprintf("That photo is too big. Pick one under %s.", sizeLabel(maxBytes)))
}

// sizeLabel writes a byte limit the way people say it ("2 MB", "512 KB").
func sizeLabel(n int64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
