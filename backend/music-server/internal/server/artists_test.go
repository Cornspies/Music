package server

import (
	"slices"
	"testing"
)

func TestSplitArtists(t *testing.T) {
	cases := map[string][]string{
		"overrated & Lieless":          {"overrated", "Lieless"},
		"Flerian, Lieless & AGONY":     {"Flerian", "Lieless", "AGONY"},
		"AGONY, Flerian &amp; Lieless": {"AGONY", "Flerian", "Lieless"},
		"deadmau5 feat. Skylar Grey":   {"deadmau5", "Skylar Grey"},
		"Lieless ft. hxvvxn":           {"Lieless", "hxvvxn"},
		"Lieless":                      {"Lieless"},
		"Lieless, lieless":             {"Lieless"},
		"Feather":                      {"Feather"},
		"":                             nil,
	}
	for in, want := range cases {
		if got := splitArtists(in); !slices.Equal(got, want) {
			t.Errorf("splitArtists(%q) = %q, want %q", in, got, want)
		}
	}
	if !hasArtist("overrated & Lieless", "lieless") || hasArtist("Lieless", "Lie") {
		t.Error("hasArtist matched wrongly")
	}
}
