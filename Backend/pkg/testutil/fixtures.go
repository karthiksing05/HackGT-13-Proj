package testutil

import (
	"Backend/pkg/contract"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Password satisfies the sign-up rule (8+ characters with a digit).
const Password = "wander2026"

// Fixed is a pinned instant for tests that assert formatted times
// (Saturday 2026-09-26 12:00 New York).
var Fixed = time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)

var emailSeq atomic.Int64

// UniqueEmail is "<name>.<n>@example.test", unique within the process.
func UniqueEmail(name string) string {
	n := emailSeq.Add(1)
	slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "."))
	if slug == "" {
		slug = "user"
	}
	return fmt.Sprintf("%s.%d@example.test", slug, n)
}

// SignupRequest is a valid sign-up for name (adult, no explicit username).
func SignupRequest(name string) contract.SignupRequest {
	dob := contract.NewTime(time.Date(2004, 5, 2, 4, 0, 0, 0, time.UTC))
	return contract.SignupRequest{Name: name, Email: UniqueEmail(name), Password: Password, DateOfBirth: &dob}
}

// Ptr returns a pointer to v (optional request fields).
func Ptr[T any](v T) *T { return &v }
