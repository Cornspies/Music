package server

import (
	"math"
	"regexp"
	"strings"
)

var accentFolds = func() map[rune]string {
	groups := map[string]string{
		"a": "àáâãäåāăą", "c": "çćĉċč", "d": "ďđð", "e": "èéêëēĕėęě", "g": "ĝğġģ", "h": "ĥħ",
		"i": "ìíîïĩīĭįı", "j": "ĵ", "k": "ķ", "l": "ĺļľŀł", "n": "ñńņňŉ", "o": "òóôõöøōŏő",
		"r": "ŕŗř", "s": "śŝşš", "t": "ţťŧ", "u": "ùúûüũūŭůűų", "w": "ŵ", "y": "ýÿŷ", "z": "źżž",
		"ss": "ß", "ae": "æ", "oe": "œ", "th": "þ",
	}
	out := map[rune]string{}
	for plain, accented := range groups {
		for _, r := range accented {
			out[r] = plain
		}
	}
	return out
}()

func foldForMatch(s string) string {
	key := normalizeForMatch(s)
	if strings.IndexFunc(key, func(r rune) bool { return r > 127 }) < 0 {
		return key
	}
	var b strings.Builder
	for _, r := range key {
		if plain, ok := accentFolds[r]; ok {
			b.WriteString(plain)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func artistMatchKey(name string) string {
	words := strings.Fields(foldForMatch(name))
	out := words[:0]
	for i, w := range words {
		if w == "and" || i == 0 && w == "the" && len(words) > 1 {
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

var matchArtistSplitRe = regexp.MustCompile(`(?i)\s*(?:,|;|&|\+|\s/\s|\s(?:feat\.?|ft\.?|featuring|x|vs\.?|with)\s)\s*`)

var trackNumberPrefixRe = regexp.MustCompile(`^\d{1,3}[\.\)]\s+`)

func matchArtists(credit string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range matchArtistSplitRe.Split(videoBracketRe.ReplaceAllString(credit, " "), -1) {
		if key := artistMatchKey(part); key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func foldedCore(title string) string { return foldForMatch(coreTitle(title)) }

type artistRelation int

const (
	artistUnknown artistRelation = iota
	artistsAgree
	artistsDiffer
)

func relateArtists(a, b string) artistRelation {
	as, bs := matchArtists(dropPlaceholder(a, "Unknown Artist")), matchArtists(dropPlaceholder(b, "Unknown Artist"))
	if len(as) == 0 || len(bs) == 0 {
		return artistUnknown
	}
	for _, x := range as {
		for _, y := range bs {
			if x == y {
				return artistsAgree
			}
		}
	}
	wholeA, wholeB := strings.Join(as, " "), strings.Join(bs, " ")
	if len(wholeA) >= 4 && len(wholeB) >= 4 && (strings.Contains(wholeA, wholeB) || strings.Contains(wholeB, wholeA)) {
		return artistsAgree
	}
	return artistsDiffer
}

type matchStrength int

const (
	matchNone matchStrength = iota
	matchWeak
	matchStrong
)

const (
	sameSongSlack      = 15.0
	unknownArtistSlack = 3.0
	otherArtistSlack   = 2.0
)

type libraryEntry struct {
	id, title, artist string
	duration          float64
}

type songLibrary map[string][]*libraryEntry

func (s *Server) loadSongLibrary(userID string) (songLibrary, error) {
	rows, err := s.db.Query(`SELECT t.id, t.title, t.artist, t.duration FROM tracks t WHERE `+sameLibrary("t.user_id"), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lib := songLibrary{}
	for rows.Next() {
		e := &libraryEntry{}
		if err := rows.Scan(&e.id, &e.title, &e.artist, &e.duration); err != nil {
			return nil, err
		}
		if key := foldedCore(e.title); key != "" {
			lib[key] = append(lib[key], e)
		}
	}
	return lib, rows.Err()
}

type songQuery struct {
	title, artist string
	duration      float64
	combined      bool
}

type nameGuess struct {
	title, artist string
	swapped       bool
}

func (q songQuery) guesses() []nameGuess {
	title := q.title
	if q.combined {
		title = cleanVideoTitle(cleanupFilename(q.title))
		if rest := trackNumberPrefixRe.ReplaceAllString(title, ""); rest != "" && videoSepRe.MatchString(rest) {
			title = rest
		}
	}
	artist := dropPlaceholder(q.artist, "Unknown Artist")
	out := []nameGuess{{title: title, artist: artist}}
	if q.combined {
		if left, right, _, ok := splitVideoTitle(title); ok {
			out = append(out, nameGuess{title: right, artist: left}, nameGuess{title: left, artist: right})
			if artist != "" {
				out = append(out, nameGuess{title: right, artist: artist}, nameGuess{title: left, artist: artist})
			}
		}
		if clean := cleanChannelName(artist); clean != artist {
			out = append(out, nameGuess{title: title, artist: clean})
		}
	}
	if artist != "" && !q.combined {
		out = append(out, nameGuess{title: artist, artist: title, swapped: true})
	}
	return out
}

type songMatch struct {
	entry    *libraryEntry
	strength matchStrength
	relation artistRelation
	reason   string
}

func sameTitleKeys(a, b string) bool {
	if foldForMatch(a) == foldForMatch(b) {
		return true
	}
	return foldedCore(a) == foldedCore(b) && titleTags(a) == titleTags(b)
}

func rateMatch(g nameGuess, q songQuery, e *libraryEntry) (matchStrength, artistRelation, string) {
	if !sameTitleKeys(g.title, e.title) {
		return matchNone, artistUnknown, ""
	}
	relation := relateArtists(g.artist, e.artist)
	if relation != artistsAgree && q.combined {
		if have := compactKey(dropPlaceholder(e.artist, "Unknown Artist")); len(have) >= 4 &&
			strings.Contains(compactKey(q.title+" "+q.artist), have) {
			relation = artistsAgree
		}
	}
	known := q.duration > 0 && e.duration > 0
	gap := math.Abs(q.duration - e.duration)
	switch {
	case relation == artistsAgree && g.swapped:
		return matchStrong, relation, "same song, with artist and title swapped"
	case relation == artistsAgree && (!known || gap <= sameSongSlack):
		return matchStrong, relation, "same song"
	case relation == artistsAgree:
		return matchWeak, relation, "same title and artist, different length"
	case relation == artistUnknown && known && gap <= unknownArtistSlack:
		return matchWeak, relation, "same title and length"
	case relation == artistsDiffer && known && gap <= otherArtistSlack && !g.swapped:
		return matchWeak, relation, "same title and length, different artist"
	}
	return matchNone, relation, ""
}

func (lib songLibrary) match(q songQuery) []songMatch {
	best := map[string]songMatch{}
	var order []string
	for _, g := range q.guesses() {
		key := foldedCore(g.title)
		if key == "" {
			continue
		}
		for _, e := range lib[key] {
			strength, relation, reason := rateMatch(g, q, e)
			if strength == matchNone {
				continue
			}
			prev, seen := best[e.id]
			if !seen {
				order = append(order, e.id)
			}
			if !seen || strength > prev.strength {
				best[e.id] = songMatch{entry: e, strength: strength, relation: relation, reason: reason}
			}
		}
	}
	out := make([]songMatch, 0, len(order))
	for _, strength := range []matchStrength{matchStrong, matchWeak} {
		for _, id := range order {
			if m := best[id]; m.strength == strength {
				out = append(out, m)
			}
		}
	}
	return out
}
