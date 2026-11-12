package server

import (
	"context"
	"testing"
)

func TestResolveVideoNames(t *testing.T) {
	known := func(name string) bool { return artistMatchKey(name) == "queen" }
	cases := []struct {
		name          string
		v             videoNames
		artist, title string
		certain       bool
	}{
		{"artist first, own channel", videoNames{Title: "Rick Astley - Never Gonna Give You Up (Official Music Video)", Channel: "Rick Astley"}, "Rick Astley", "Never Gonna Give You Up", true},
		{"title first, own channel", videoNames{Title: "Never Gonna Give You Up - Rick Astley (Lyrics)", Channel: "Rick Astley"}, "Rick Astley", "Never Gonna Give You Up", true},
		{"hyphenated artist", videoNames{Title: "JAY-Z - Empire State Of Mind ft. Alicia Keys", Channel: "JAY-Z"}, "JAY-Z", "Empire State Of Mind ft. Alicia Keys", true},
		{"hyphen and digits", videoNames{Title: "blink-182 - All The Small Things (Official Music Video)", Channel: "blink-182"}, "blink-182", "All The Small Things", true},
		{"vevo channel", videoNames{Title: "Ne-Yo - So Sick (Official Music Video)", Channel: "NeYoVEVO"}, "Ne-Yo", "So Sick", true},
		{"square brackets", videoNames{Title: "Ed Sheeran - Shape of You [Official Video]", Channel: "Ed Sheeran"}, "Ed Sheeran", "Shape of You", true},
		{"topic channel", videoNames{Title: "Blinding Lights", Channel: "The Weeknd - Topic"}, "The Weeknd", "Blinding Lights", true},
		{"yt-dlp fields win", videoNames{Title: "The Weeknd - Blinding Lights (Official Video)", Channel: "TheWeekndVEVO", Track: "Blinding Lights", Artists: []string{"The Weeknd"}}, "The Weeknd", "Blinding Lights", true},
		{"track tells the sides apart", videoNames{Title: "Blinding Lights - The Weeknd (Lyrics)", Channel: "LyricsHub", Track: "Blinding Lights"}, "The Weeknd", "Blinding Lights", true},
		{"artist tells the sides apart", videoNames{Title: "Blinding Lights | The Weeknd", Channel: "LyricsHub", Artists: []string{"The Weeknd"}}, "The Weeknd", "Blinding Lights", true},
		{"quoted title", videoNames{Title: `Queen "Bohemian Rhapsody" (Official Video Remastered)`, Channel: "Queen Official"}, "Queen", "Bohemian Rhapsody", true},
		{"altered version keeps its tag", videoNames{Title: "Artist - Song (Slowed + Reverb)", Channel: "Chill", Track: "Song", Artists: []string{"Artist"}}, "Artist", "Song (Slowed + Reverb)", true},
		{"live tag survives", videoNames{Title: "Imagine Dragons - Believer (Live Video)", Channel: "ImagineDragonsVEVO"}, "Imagine Dragons", "Believer (Live Video)", true},
		{"artist already in the library", videoNames{Title: "Bohemian Rhapsody - Queen", Channel: "MusicLover99"}, "Queen", "Bohemian Rhapsody", true},
		{"pipe with packaging", videoNames{Title: "Dua Lipa - Levitating | Official Lyric Video", Channel: "Dua Lipa"}, "Dua Lipa", "Levitating", true},
		{"colon after the channel's name", videoNames{Title: "Panic! At The Disco: High Hopes [OFFICIAL VIDEO]", Channel: "Panic! At The Disco"}, "Panic! At The Disco", "High Hopes", true},
		{"no separator", videoNames{Title: "lofi hip hop radio", Channel: "Lofi Girl"}, "Lofi Girl", "lofi hip hop radio", true},
		{"unspaced en dash", videoNames{Title: "Daft Punk–Around the World", Channel: "Daft Punk"}, "Daft Punk", "Around the World", true},
		{"several artists", videoNames{Title: "Calvin Harris, Dua Lipa - One Kiss (Official Video)", Channel: "CalvinHarrisVEVO"}, "Calvin Harris, Dua Lipa", "One Kiss", true},
		{"title by artist", videoNames{Title: "Shallow by Lady Gaga", Channel: "Lady Gaga"}, "Lady Gaga", "Shallow", true},
		{"featured artist stays", videoNames{Title: "Post Malone - Sunflower (feat. Swae Lee)", Channel: "Post Malone"}, "Post Malone", "Sunflower (feat. Swae Lee)", true},
		{"html entities", videoNames{Title: "Guns N&#39; Roses - Sweet Child O&#39; Mine (Official Music Video)", Channel: "Guns N' Roses"}, "Guns N' Roses", "Sweet Child O' Mine", true},
		{"trailing packaging words", videoNames{Title: "Coldplay - Yellow Official Video", Channel: "Coldplay"}, "Coldplay", "Yellow", true},
		{"hyphenated title, no separator", videoNames{Title: "Twenty-One (Audio)", Channel: "Khalid"}, "Khalid", "Twenty-One", true},
		{"label channel, nothing to go on", videoNames{Title: "Twenty One Pilots - Stressed Out [OFFICIAL VIDEO]", Channel: "Fueled By Ramen"}, "Twenty One Pilots", "Stressed Out", false},
		{"lyrics channel, nothing to go on", videoNames{Title: "Shape of You - Ed Sheeran (Lyrics)", Channel: "7clouds"}, "Shape of You", "Ed Sheeran", false},
		{"collaboration hints at the artist side", videoNames{Title: "Sunflower - Post Malone ft. Swae Lee", Channel: "Vibes"}, "Post Malone ft. Swae Lee", "Sunflower", false},
	}
	for _, c := range cases {
		res := resolveVideoNames(c.v, known)
		if res.artist != c.artist || res.title != c.title || res.certain != c.certain {
			t.Errorf("%s: %q by %q (certain %v), want %q by %q (certain %v)", c.name, res.title, res.artist, res.certain, c.title, c.artist, c.certain)
		}
	}
}

func catalogue(entries ...[2]string) nameSearch {
	return func(context.Context, string) ([]metadataCandidate, error) {
		out := make([]metadataCandidate, 0, len(entries))
		for _, e := range entries {
			out = append(out, metadataCandidate{artist: e[0], title: e[1]})
		}
		return out, nil
	}
}

func TestConfirmNamesOnline(t *testing.T) {
	resolve := func(title, channel string, search nameSearch) nameResolution {
		return confirmNamesOnline(context.Background(), resolveVideoNames(videoNames{Title: title, Channel: channel}, nil), []nameSearch{search})
	}

	swapped := resolve("shape of you - ed sheeran (Lyrics)", "7clouds", catalogue([2]string{"Ed Sheeran", "Shape of You"}, [2]string{"Ed Sheeran", "Shape of You (Acoustic)"}))
	if swapped.artist != "Ed Sheeran" || swapped.title != "Shape of You" || !swapped.certain {
		t.Errorf("title-first video: got %q by %q (certain %v)", swapped.title, swapped.artist, swapped.certain)
	}

	straight := resolve("twenty one pilots - stressed out [OFFICIAL VIDEO]", "Fueled By Ramen", catalogue([2]string{"Twenty One Pilots", "Stressed Out"}))
	if straight.artist != "Twenty One Pilots" || straight.title != "Stressed Out" || !straight.certain {
		t.Errorf("artist-first video: got %q by %q (certain %v)", straight.title, straight.artist, straight.certain)
	}

	styled := resolve("Gangnam Style - PSY", "KpopFan", catalogue([2]string{"Psy", "Gangnam Style"}))
	if styled.artist != "PSY" || styled.title != "Gangnam Style" {
		t.Errorf("the video's own spelling was replaced: got %q by %q", styled.title, styled.artist)
	}

	featured := resolve("Heroes Tonight - Janji (feat. Johnning) [NCS Release]", "Random", catalogue([2]string{"Janji", "Heroes Tonight (feat. Johnning)"}))
	if tidyArtist(featured.artist) != "Janji feat. Johnning" || featured.title != "Heroes Tonight" {
		t.Errorf("featured artist on the artist side: got %q by %q", featured.title, tidyArtist(featured.artist))
	}

	collab := resolve("Sunflower - Post Malone ft. Swae Lee", "Vibes", catalogue([2]string{"Post Malone", "Sunflower (Spider-Man: Into the Spider-Verse)"}))
	if collab.artist != "Post Malone ft. Swae Lee" || collab.title != "Sunflower" {
		t.Errorf("collaboration: got %q by %q", collab.title, collab.artist)
	}

	unrelated := resolve("My Holiday - Day Three", "Travel Vlogs", catalogue([2]string{"Someone", "Holiday"}, [2]string{"Day Three", "Other Song"}))
	if unrelated.artist != "My Holiday" || unrelated.title != "Day Three" || unrelated.certain {
		t.Errorf("no catalogue match: got %q by %q (certain %v)", unrelated.title, unrelated.artist, unrelated.certain)
	}

	selfTitled := resolve("Bad Company - Bad Company", "Classic Rock", catalogue([2]string{"Bad Company", "Bad Company"}))
	if selfTitled.artist != "Bad Company" || selfTitled.title != "Bad Company" {
		t.Errorf("self-titled song: got %q by %q", selfTitled.title, selfTitled.artist)
	}
}
