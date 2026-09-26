package itinerary

import (
	"Backend/pkg/travel"
	"testing"
)

func TestResolveMode(t *testing.T) {
	cases := []struct {
		ride  string
		modes []string
		want  travel.Mode
		label string
	}{
		{"", nil, travel.Walk, "drive"},
		{"rideshare", nil, travel.Drive, "rideshare"},
		{"", []string{"walk", "drive"}, travel.Drive, "drive"},
		{"none", []string{"marta"}, travel.Transit, "drive"},
	}
	for _, c := range cases {
		m, l := ResolveMode(c.ride, c.modes)
		if m != c.want || l != c.label {
			t.Errorf("ResolveMode(%q, %v) = %s/%s, want %s/%s", c.ride, c.modes, m, l, c.want, c.label)
		}
	}
}
