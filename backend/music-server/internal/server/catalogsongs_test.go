package server

import "testing"

func hit(source, title string, duration float64, year int) catalogHit {
	return catalogHit{discoverResult: discoverResult{Source: source, ID: source + title, Title: title, Artist: "Tristam", Duration: duration}, year: year}
}

func TestMergeCatalogSongs(t *testing.T) {
	songs := mergeCatalogSongs([][]catalogHit{
		{hit("deezer", "Frame of Mind", 274, 2014), hit("deezer", "Flight", 219, 2013)},
		{hit("soundcloud", "Frame of Mind", 275, 2014), hit("soundcloud", "Frame of Mind (VIP)", 280, 2015)},
		{hit("youtube", "Frame Of Mind", 0, 0), hit("youtube", "Flight", 330, 0), hit("youtube", "Bone Dry", 200, 0)},
	})
	got := map[string]int{}
	for _, s := range songs {
		got[s.Title] += len(s.Found)
	}
	want := map[string]int{"Frame of Mind": 3, "Flight": 1, "Frame of Mind (VIP)": 1, "Bone Dry": 1}
	for title, n := range want {
		if got[title] < n {
			t.Errorf("%q found on %d sources, want %d (all: %v)", title, got[title], n, got)
		}
	}
	if len(songs) != 5 {
		t.Errorf("got %d songs, want 5 (a 330s Flight is a different version)", len(songs))
	}
	if songs[0].Title != "Frame of Mind (VIP)" || songs[len(songs)-1].Year != 0 {
		t.Errorf("want newest first and unknown years last, got %+v", songs)
	}
}
