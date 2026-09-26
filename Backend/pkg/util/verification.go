package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
)

var (
	nameRegExp     = regexp.MustCompile(`^[a-zA-Z0-9_\s]{2,50}$`)
	usernameRegExp = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,30}$`)
	emailRegExp    = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
)

func IsValidName(name string) bool {
	return nameRegExp.MatchString(name)
}

func IsValidUsername(username string) bool {
	return usernameRegExp.MatchString(username)
}

func IsValidEmail(email string) bool {
	return emailRegExp.MatchString(email)
}

func IsValidPassword(password string) bool {
	return len(password) >= 6
}

func GenerateSixDigitCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "123456"
	}
	return fmt.Sprintf("%06d", n.Int64()+100000)
}
