package server

import "testing"

func TestSameSongPairVariantTags(t *testing.T) {
	track := func(title, artist string, duration float64) *Track {
		return &Track{Title: title, Artist: artist, Duration: duration}
	}
	cases := []struct {
		a, b *Track
		want bool
	}{
		{track(`C:\\AR?A_M4TH (AMB13NT)`, "bxnwxghxrn", 379.6), track(`C:\\AR?A_M4TH`, "bxnwxghxrn", 380), false},
		{track(`C:\\AR?A_M4TH (AMB13NT)`, "bxnwxghxrn", 379.6), track(`C__AR_A_M4TH (AMB13NT)`, "bxnwxghxrn", 379.6), true},
		{track(`C:\\AR?A_M4TH`, "bxnwxghxrn", 380), track(`C__AR_A_M4TH`, "bxnwxghxrn", 380), true},
		{track(`C:\\AR?A_M4TH (AMB13NT)`, "bxnwxghxrn", 379.6), track(`C AR A M4TH  AMB13NT`, "bxnwxghxrn", 379.6), true},
		{track("Enemy (Official Video)", "Imagine Dragons", 173), track("Enemy", "Imagine Dragons", 174), true},
		{track("Enemy (from the series Arcane League of Legends)", "Imagine Dragons", 173), track("Enemy", "Imagine Dragons", 173), true},
		{track("Far Away (feat. Someone) [NCS Release]", "Tristam", 270), track("Far Away", "Tristam", 271), true},
		{track("Song (Remastered 2011)", "Band", 200), track("Song", "Band", 200), true},
		{track("Song (Live)", "Band", 200), track("Song", "Band", 200), false},
		{track("Song (Slowed)", "Band", 200), track("Song (Live)", "Band", 200), false},
	}
	for _, c := range cases {
		if got, _ := sameSongPair(c.a, c.b); got != c.want {
			t.Errorf("sameSongPair(%q, %q) = %v, want %v", c.a.Title, c.b.Title, got, c.want)
		}
	}
}

func TestSameRecordingVariantTags(t *testing.T) {
	own := upgradeTrack{title: `C:\\AR?A_M4TH`, artist: "bxnwxghxrn", duration: 380}
	if sameRecording(own, discoverResult{Title: `C:\\AR?A_M4TH (AMB13NT)`, Artist: "bxnwxghxrn", Duration: 380}) {
		t.Error("an altered version was taken as the same recording")
	}
	if !sameRecording(own, discoverResult{Title: `bxnwxghxrn - C:\\AR?A_M4TH (Official Audio)`, Artist: "bxnwxghxrn", Duration: 381}) {
		t.Error("the same recording with a packaging tag was rejected")
	}
}
