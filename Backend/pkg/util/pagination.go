package util

import (
	"encoding/base64"
	"net/http"
	"strconv"
)

type PaginatedResponse struct {
	Items      interface{} `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
	Total      int         `json:"total,omitempty"`
}

func ParsePagination(r *http.Request, defaultLimit int) (cursor string, limit int) {
	q := r.URL.Query()
	cursor = q.Get("cursor")

	limit = defaultLimit
	if limit <= 0 {
		limit = 20
	}

	if lStr := q.Get("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}

	return cursor, limit
}

func EncodeCursor(s string) string {
	if s == "" {
		return ""
	}
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func DecodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
