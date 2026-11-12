package server

import (
	"context"
	"testing"
)

func testLibrary() songLibrary {
	lib := songLibrary{}
	for _, e := range []*libraryEntry{
		{id: "shape", title: "Shape of You", artist: "Ed Sheeran", duration: 233},
		{id: "lights", title: "Blinding Lights", artist: "The Weeknd", duration: 200},
		{id: "swapped", title: "Queen", artist: "Bohemian Rhapsody", duration: 354},
		{id: "cafe", title: "Café del Mar", artist: "Energy 52", duration: 420},
		{id: "home", title: "Home", artist: "Edward Sharpe & The Magnetic Zeros", duration: 303},
		{id: "live", title: "Song (Live)", artist: "Band", duration: 200},
		{id: "numb", title: "Numb / Encore", artist: "JAY-Z, Linkin Park", duration: 205},
		{id: "untagged", title: "Yellow", artist: "Unknown Artist", duration: 266},
	} {
		lib[foldedCore(e.title)] = append(lib[foldedCore(e.title)], e)
	}
	return lib
}

func TestSongLibraryMatch(t *testing.T) {
	lib := testLibrary()
	cases := []struct {
		name     string
		q        songQuery
		id       string
		strength matchStrength
	}{
		{"official video, same length", songQuery{title: "Ed Sheeran - Shape of You (Official Music Video)", artist: "Ed Sheeran", duration: 240, combined: true}, "shape", matchStrong},
		{"official video with a long intro", songQuery{title: "Ed Sheeran - Shape of You (Official Music Video)", artist: "Ed Sheeran", duration: 263, combined: true}, "shape", matchWeak},
		{"title-first lyrics video", songQuery{title: "Shape of You - Ed Sheeran (Lyrics)", artist: "7clouds", duration: 234, combined: true}, "shape", matchStrong},
		{"topic channel", songQuery{title: "Blinding Lights", artist: "The Weeknd - Topic", duration: 201, combined: true}, "lights", matchStrong},
		{"video against a swapped library entry", songQuery{title: "Queen - Bohemian Rhapsody (Official Video Remastered)", artist: "Queen Official", duration: 359, combined: true}, "swapped", matchStrong},
		{"clean tags against a swapped library entry", songQuery{title: "Bohemian Rhapsody", artist: "Queen", duration: 354}, "swapped", matchStrong},
		{"accents and case", songQuery{title: "CAFE DEL MAR", artist: "Energy 52", duration: 421}, "cafe", matchStrong},
		{"artists in another order", songQuery{title: "Numb/Encore", artist: "Linkin Park & Jay-Z", duration: 205}, "numb", matchStrong},
		{"file name with a track number", songQuery{title: "02 - The Weeknd - Blinding Lights", duration: 200, combined: true}, "lights", matchStrong},
		{"underscored file name", songQuery{title: "The_Weeknd_-_Blinding_Lights", duration: 200, combined: true}, "lights", matchStrong},
		{"same title and length, other artist", songQuery{title: "Home", artist: "Daughtry", duration: 304}, "home", matchWeak},
		{"same title, other artist, other length", songQuery{title: "Home", artist: "Daughtry", duration: 250}, "", matchNone},
		{"studio version against a live one", songQuery{title: "Song", artist: "Band", duration: 200}, "", matchNone},
		{"remix of a song in the library", songQuery{title: "Shape of You (Galantis Remix)", artist: "Ed Sheeran", duration: 233}, "", matchNone},
		{"unknown artist, same length", songQuery{title: "Blinding Lights", duration: 201}, "lights", matchWeak},
		{"unknown artist, unknown length", songQuery{title: "Blinding Lights"}, "", matchNone},
		{"untagged library track", songQuery{title: "Coldplay - Yellow (Official Video)", artist: "Coldplay", duration: 267, combined: true}, "untagged", matchWeak},
		{"another song", songQuery{title: "Perfect", artist: "Ed Sheeran", duration: 263}, "", matchNone},
	}
	for _, c := range cases {
		matches := lib.match(c.q)
		id, strength := "", matchNone
		if len(matches) > 0 {
			id, strength = matches[0].entry.id, matches[0].strength
		}
		if id != c.id || strength != c.strength {
			t.Errorf("%s: matched %q (strength %d), want %q (strength %d)", c.name, id, strength, c.id, c.strength)
		}
	}
}

func TestSwappedPair(t *testing.T) {
	a := &Track{Title: "Bohemian Rhapsody", Artist: "Queen"}
	b := &Track{Title: "Queen", Artist: "Bohemian Rhapsody"}
	if !swappedPair(a, b) || !swappedPair(b, a) {
		t.Error("a pair with artist and title swapped was not recognised")
	}
	if swappedPair(a, &Track{Title: "Queen", Artist: "Queen"}) {
		t.Error("an unrelated pair was taken as swapped")
	}
	if swappedPair(&Track{Title: "Bad Company", Artist: "Bad Company"}, &Track{Title: "Bad Company", Artist: "Bad Company"}) {
		t.Error("a self-titled song was taken as swapped")
	}
}

func seedLibrary(t *testing.T, s *Server) string {
	t.Helper()
	const userID = "match-test-user"
	if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash, created_at, role) VALUES (?,?,?,?,?)`,
		userID, "matcher", "!", nowRFC3339(), roleUser); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		id, title, artist, source, sourceID string
		duration                            float64
	}{
		{"shape", "Shape of You", "Ed Sheeran", "", "", 233},
		{"perfect", "Perfect", "Ed Sheeran", "", "", 263},
		{"lights", "Blinding Lights", "The Weeknd", "youtube", "4NRXx6U8ABQ", 200},
		{"swapped", "Queen", "Bohemian Rhapsody", "youtube", "fJ9rUzIMcZQ", 354},
		{"home", "Home", "Edward Sharpe & The Magnetic Zeros", "", "", 303},
	} {
		if _, err := s.db.Exec(`INSERT INTO tracks (id, user_id, title, artist, album, duration, file_name, file_path, mime_type, file_size, created_at, source, source_id)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.id, userID, e.title, e.artist, "Unknown Album", e.duration, e.id+".mp3", "uploads/"+e.id+".mp3", "audio/mpeg", 1, nowRFC3339(), e.source, e.sourceID); err != nil {
			t.Fatal(err)
		}
	}
	return userID
}

func TestDiscoverMarksSongsAlreadyInLibrary(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	results := []discoverResult{
		{Source: "youtube", ID: "4NRXx6U8ABQ", Title: "The Weeknd - Blinding Lights (Official Video)", Artist: "TheWeekndVEVO", Duration: 262},
		{Source: "youtube", ID: "JGwWNGJdvx8", Title: "Ed Sheeran - Shape of You (Official Music Video)", Artist: "Ed Sheeran", Duration: 240},
		{Source: "youtube", ID: "aaaaaaaaaaa", Title: "Shape of You - Ed Sheeran (Lyrics)", Artist: "7clouds", Duration: 234},
		{Source: "youtube", ID: "bbbbbbbbbbb", Title: "Ed Sheeran - Shape of You (Live at the BRITs)", Artist: "BRITs", Duration: 245},
		{Source: "deezer", ID: "1", Title: "Bohemian Rhapsody", Artist: "Queen", Duration: 354},
		{Source: "deezer", ID: "2", Title: "Home", Artist: "Daughtry", Duration: 303},
		{Source: "deezer", ID: "3", Title: "Thinking Out Loud", Artist: "Ed Sheeran", Duration: 281},
		{Source: "youtube", ID: "ccccccccccc", Title: "Blinding Lights (Lyrics)", Artist: "SomeLyrics", Duration: 201},
	}
	s.markInLibrary(userID, results)
	want := []struct{ in, similar string }{
		{"lights", ""}, {"shape", ""}, {"shape", ""}, {"", ""}, {"swapped", ""}, {"", "home"}, {"", ""}, {"", "lights"},
	}
	for i, w := range want {
		if results[i].TrackID != w.in || results[i].SimilarID != w.similar {
			t.Errorf("%q: in library %q, similar %q; want %q, %q", results[i].Title, results[i].TrackID, results[i].SimilarID, w.in, w.similar)
		}
	}
}

func TestUploadDuplicateCheckReadsFileNames(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	cases := []struct {
		q      songQuery
		id     string
		reason string
	}{
		{songQuery{title: "Ed Sheeran - Shape of You (Official Music Video)", duration: 234, combined: true}, "shape", "same song"},
		{songQuery{title: "03. Ed_Sheeran_-_Perfect", duration: 263, combined: true}, "perfect", "same song"},
		{songQuery{title: "Bohemian Rhapsody", artist: "Queen", duration: 355}, "swapped", "same song, with artist and title swapped"},
		{songQuery{title: "Home", artist: "Daughtry", duration: 303}, "home", "same title and length, different artist"},
		{songQuery{title: "Photograph", artist: "Ed Sheeran", duration: 258}, "", ""},
	}
	for _, c := range cases {
		got := s.findSameSong(userID, c.q, true)
		id, reason := "", ""
		if len(got) > 0 {
			id, reason = got[0].Track.ID, got[0].Reason
		}
		if id != c.id || reason != c.reason {
			t.Errorf("%q: got %q (%s), want %q (%s)", c.q.title, id, reason, c.id, c.reason)
		}
	}
	if got := s.findSameSong(userID, songQuery{title: "Home", artist: "Daughtry", duration: 303}, false); len(got) != 0 {
		t.Errorf("a different artist's song was offered as a copy: %+v", got[0].Track.Title)
	}
}

func TestVideoNamesUseArtistsFromTheLibrary(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	searched := false
	saved := nameSearchers
	nameSearchers = []nameSearch{func(context.Context, string) ([]metadataCandidate, error) {
		searched = true
		return nil, nil
	}}
	t.Cleanup(func() { nameSearchers = saved })

	artist, title := s.videoNames(context.Background(), userID, videoNames{Title: "Photograph - Ed Sheeran (Lyrics)", Channel: "7clouds"})
	if artist != "Ed Sheeran" || title != "Photograph" {
		t.Errorf("got %q by %q", title, artist)
	}
	if searched {
		t.Error("the catalogues were asked although the library already settled it")
	}
	artist, title = s.videoNames(context.Background(), userID, videoNames{Title: "Unknown Band - Unknown Song", Channel: "promo"})
	if artist != "Unknown Band" || title != "Unknown Song" || !searched {
		t.Errorf("got %q by %q (searched %v)", title, artist, searched)
	}
}
