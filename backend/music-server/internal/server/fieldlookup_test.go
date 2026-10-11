package server

import "testing"

func TestAllowedArtworkURL(t *testing.T) {
	ok := []string{
		"https://cdn-images.dzcdn.net/images/cover/x/500x500.jpg",
		"https://is3-ssl.mzstatic.com/image/thumb/x/600x600bb.jpg",
		"https://coverartarchive.org/release/abc/front-250",
		"https://i.ytimg.com/vi/abc/hqdefault.jpg",
		"https://i1.sndcdn.com/artworks-x-t500x500.jpg",
	}
	bad := []string{
		"http://cdn-images.dzcdn.net/images/x.jpg",
		"https://evil-dzcdn.net/x.jpg",
		"https://dzcdn.net.evil.com/x.jpg",
		"https://user@i.ytimg.com/x.jpg",
		"https://127.0.0.1/x.jpg",
		"file:///etc/passwd",
		"not a url",
	}
	for _, u := range ok {
		if !allowedArtworkURL(u) {
			t.Errorf("%s should be allowed", u)
		}
	}
	for _, u := range bad {
		if allowedArtworkURL(u) {
			t.Errorf("%s should be refused", u)
		}
	}
}

func TestFilterFieldOptions(t *testing.T) {
	opts := []FieldOption{
		{Title: "Feel So Close", Artist: "Calvin Harris", Album: "Now That's What I Call Music 80", Year: 2011, Duration: 206},
		{Title: "Feel So Close (Radio Edit)", Artist: "Calvin Harris", Album: "18 Months", Year: 2012, Duration: 206},
		{Title: "Feel So Close", Artist: "Calvin Harris", Album: "Feel So Close", Year: 2011, Duration: 207},
		{Title: "Feel So Close", Artist: "Calvin Harris", Album: "Feel So Close", Year: 2011, Duration: 207},
		{Title: "Feel So Close (Nero Remix)", Artist: "Calvin Harris", Album: "Feel So Close (Remixes)", Year: 2011},
		{Title: "Feel So Close", Artist: "Cover Band", Album: "Covers", Year: 2015, Duration: 206},
		{Title: "Feel So Close", Artist: "Calvin Harris", Album: "Extended", Year: 2013, Duration: 400},
		{Title: "Feel So Close", Artist: "Calvin Harris", Album: "Unknown date"},
	}
	got := filterFieldOptions(opts, "Feel So Close", "Calvin Harris", 206)
	want := []string{"Feel So Close", "Now That's What I Call Music 80", "18 Months", "Unknown date"}
	if len(got) != len(want) {
		t.Fatalf("got %d options: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Album != w {
			t.Errorf("option %d = %q, want %q", i, got[i].Album, w)
		}
	}
}
