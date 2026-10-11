package server

import (
	"html"
	"regexp"
	"strings"
)

var artistSplitRe = regexp.MustCompile(`(?i)\s*(?:,|;|&|\s(?:feat\.?|ft\.?|featuring)\s)\s*`)

func splitArtists(artist string) []string {
	artist = strings.TrimSpace(html.UnescapeString(artist))
	if artist == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, part := range artistSplitRe.Split(artist, -1) {
		part = strings.TrimSpace(part)
		key := normalizeForMatch(part)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, part)
	}
	if len(out) == 0 {
		return []string{artist}
	}
	return out
}

func artistKey(name string) string { return normalizeForMatch(name) }

func hasArtist(credit, name string) bool {
	want := artistKey(name)
	if want == "" {
		return false
	}
	for _, a := range splitArtists(credit) {
		if artistKey(a) == want {
			return true
		}
	}
	return false
}

func artistsOverlap(a, b string) bool {
	for _, x := range splitArtists(a) {
		if hasArtist(b, x) {
			return true
		}
	}
	return false
}
