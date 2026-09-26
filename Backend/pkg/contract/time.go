package contract

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Time is a wire timestamp. It marshals as RFC 3339 in UTC without fractional
// seconds ("2026-09-25T18:10:00Z") and unmarshals RFC 3339 with or without
// fractions and any offset, or a day-only "2006-01-02" (UTC midnight, which
// handlers re-anchor in the request time zone). A zero non-pointer Time is
// still written; optional fields are *Time with omitempty. In BSON it is a
// plain datetime.
type Time struct {
	time.Time
}

const dayLayout = "2006-01-02"

// NewTime wraps a time.Time.
func NewTime(t time.Time) Time { return Time{Time: t} }

// Ptr wraps a time.Time for an optional field.
func Ptr(t time.Time) *Time { return &Time{Time: t} }

// PtrOrNil wraps an optional time.Time, keeping nil.
func PtrOrNil(t *time.Time) *Time {
	if t == nil {
		return nil
	}
	return &Time{Time: *t}
}

// Std returns the wrapped time.Time.
func (t Time) Std() time.Time { return t.Time }

// StdPtr returns the wrapped time as an optional time.Time.
func (t *Time) StdPtr() *time.Time {
	if t == nil {
		return nil
	}
	return &t.Time
}

// ParseTime accepts the formats the app sends.
func ParseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(dayLayout, s); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("bad time %q", s)
}

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(t.UTC().Format(time.RFC3339))), nil
}

func (t *Time) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return fmt.Errorf("time must be a string: %w", err)
	}
	parsed, err := ParseTime(s)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

func (t Time) MarshalBSONValue() (byte, []byte, error) {
	typ, data, err := bson.MarshalValue(t.Time)
	return byte(typ), data, err
}

func (t *Time) UnmarshalBSONValue(typ byte, data []byte) error {
	return bson.UnmarshalValue(bson.Type(typ), data, &t.Time)
}
