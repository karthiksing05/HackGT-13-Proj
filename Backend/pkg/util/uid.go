package util

import (
	"github.com/google/uuid"
)

func GenerateID() string {
	u, err := uuid.NewV7()
	if err != nil {
		return uuid.New().String()
	}
	return u.String()
}
