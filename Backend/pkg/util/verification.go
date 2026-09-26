package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	usernameRegExp = regexp.MustCompile(`^[a-z0-9_.]{3,30}$`)
	emailRegExp    = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
)

// IsValidName accepts a trimmed display name of 2–80 characters without control characters.
func IsValidName(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < 2 || n > 80 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// IsValidUsername checks an already normalized handle (see NormalizeUsername).
func IsValidUsername(username string) bool {
	return usernameRegExp.MatchString(username)
}

func IsValidEmail(email string) bool {
	return len(email) <= 254 && emailRegExp.MatchString(email)
}

// IsValidPassword is the app's rule: at least 8 characters including a digit.
func IsValidPassword(password string) bool {
	if utf8.RuneCountInString(password) < 8 {
		return false
	}
	for _, r := range password {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// NormalizeEmail lowercases and trims an address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeUsername strips a leading "@" and surrounding spaces and lowercases
// the handle; ok is false when the result is not ^[a-z0-9_.]{3,30}$.
func NormalizeUsername(raw string) (handle string, ok bool) {
	handle = strings.ToLower(strings.Trim(strings.TrimSpace(raw), "@ "))
	return handle, IsValidUsername(handle)
}

// GenerateUsername builds "<name>_<4hex>" from the first word of a display
// name (letters and digits only, "user" when nothing survives).
func GenerateUsername(name string) string {
	base := strings.Builder{}
	first := strings.Fields(strings.ToLower(name))
	if len(first) > 0 {
		for _, r := range first[0] {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				base.WriteRune(r)
			}
			if base.Len() >= 24 {
				break
			}
		}
	}
	b := base.String()
	if b == "" {
		b = "user"
	}
	return b + "_" + RandomHex(2)
}

// GenerateSixDigitCode returns a zero-padded code in 100000–999999.
func GenerateSixDigitCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		panic("util: crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%06d", n.Int64()+100000)
}
