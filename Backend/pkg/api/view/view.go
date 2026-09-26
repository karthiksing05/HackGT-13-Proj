// Package view renders people the same way in every handler: PersonRef and
// User from a users document, initials, the avatar palette and the age
// bracket. Time and money labels live in pkg/httpx (fmt.go).
package view

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"strings"
	"time"
)

// Palette is the app's avatar colors (frontend DesignSystem/Colors.swift).
var Palette = map[contract.AvatarColor]string{
	contract.AvatarInk:    "#18211C",
	contract.AvatarSage:   "#899E88",
	contract.AvatarClay:   "#C77B58",
	contract.AvatarForest: "#4A5F49",
	contract.AvatarSand:   "#D9CBB0",
}

// ColorHex maps an avatar color name to its hex; unknown reads as ink.
func ColorHex(color string) string {
	if hex, ok := Palette[contract.AvatarColor(color)]; ok {
		return hex
	}
	return Palette[contract.AvatarInk]
}

// Initials are the first letters of the first two words, uppercased; "" for
// an empty name (never someone else's initials).
func Initials(name string) string {
	var b strings.Builder
	for i, word := range strings.Fields(name) {
		if i == 2 {
			break
		}
		for _, r := range word {
			b.WriteRune(r)
			break
		}
	}
	return strings.ToUpper(b.String())
}

// PhotoURL is the public avatar URL when the user has a photo.
func PhotoURL(u *models.User, publicBaseURL string) *string {
	if u.PhotoID == nil || *u.PhotoID == "" {
		return nil
	}
	url := strings.TrimRight(publicBaseURL, "/") + "/photos/" + *u.PhotoID
	return &url
}

// StrPtr returns nil for "" so optional strings are omitted, not empty.
func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PersonRef is how a user appears to everyone else.
func PersonRef(u *models.User, publicBaseURL string) contract.PersonRef {
	return contract.PersonRef{
		ID:       u.ID.Hex(),
		Name:     u.Name,
		Initials: Initials(u.Name),
		ColorHex: ColorHex(u.AvatarColor),
		PhotoURL: PhotoURL(u, publicBaseURL),
		Username: StrPtr(u.Username),
	}
}

// User is GET /me; the age bracket is computed from the birth date at now.
func User(u *models.User, publicBaseURL string, now time.Time) contract.User {
	return contract.User{
		ID:            u.ID.Hex(),
		Name:          u.Name,
		Username:      StrPtr(u.Username),
		Email:         u.Email,
		PhotoURL:      PhotoURL(u, publicBaseURL),
		AvatarColor:   contract.AvatarColor(u.AvatarColor),
		Status:        contract.PresenceStatus(u.Status),
		AgeBracket:    AgeBracket(u.BirthDate, now),
		School:        u.School,
		SetupComplete: u.SetupComplete,
		HomeBase:      HomeBase(u.HomeBase),
		City:          StrPtr(u.City),
	}
}

// HomeBase renders the stored home base as a Place.
func HomeBase(hb *models.HomeBase) *contract.Place {
	if hb == nil {
		return nil
	}
	return &contract.Place{Name: hb.Name, Coordinate: &contract.Coordinate{Lat: hb.Lat, Lng: hb.Lng}}
}

// Age is the number of whole years between birth and now (UTC calendar).
func Age(birth, now time.Time) int {
	birth, now = birth.UTC(), now.UTC()
	years := now.Year() - birth.Year()
	if now.Month() < birth.Month() || (now.Month() == birth.Month() && now.Day() < birth.Day()) {
		years--
	}
	return years
}

// AgeBracket is under_13 / teen / under_21 / adult; no birth date reads as adult.
func AgeBracket(birth *time.Time, now time.Time) contract.AgeBracket {
	if birth == nil {
		return contract.AgeAdult
	}
	switch age := Age(*birth, now); {
	case age < 13:
		return contract.AgeUnder13
	case age < 18:
		return contract.AgeTeen
	case age < 21:
		return contract.AgeUnder21
	}
	return contract.AgeAdult
}
