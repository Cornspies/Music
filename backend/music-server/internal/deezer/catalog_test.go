package deezer

import (
	"context"
	"testing"
)

func TestParseURL(t *testing.T) {
	c := &Client{}
	ctx := context.Background()

	cases := []struct {
		url      string
		wantKind string
		wantID   string
	}{
		{"https://www.deezer.com/track/3135556", "track", "3135556"},
		{"https://www.deezer.com/en/track/12345678", "track", "12345678"},
		{"https://deezer.com/fr/album/456789", "album", "456789"},
		{"https://www.deezer.com/playlist/987654321", "playlist", "987654321"},
		{"https://www.deezer.com/artist/112233", "artist", "112233"},
	}

	for _, tc := range cases {
		kind, id, err := c.ParseURL(ctx, tc.url)
		if err != nil {
			t.Errorf("ParseURL(%q) unexpected error: %v", tc.url, err)
			continue
		}
		if kind != tc.wantKind || id != tc.wantID {
			t.Errorf("ParseURL(%q) = (%q, %q), want (%q, %q)", tc.url, kind, id, tc.wantKind, tc.wantID)
		}
	}
}

func TestQualities(t *testing.T) {
	if QualityCode(QualityFLAC) != 9 {
		t.Errorf("expected FLAC code 9, got %d", QualityCode(QualityFLAC))
	}
	if QualityCode(QualityMP3_320) != 3 {
		t.Errorf("expected MP3_320 code 3, got %d", QualityCode(QualityMP3_320))
	}
	if QualityCode(QualityMP3_128) != 1 {
		t.Errorf("expected MP3_128 code 1, got %d", QualityCode(QualityMP3_128))
	}

	if QualityExt(QualityFLAC) != ".flac" {
		t.Errorf("expected .flac, got %s", QualityExt(QualityFLAC))
	}
	if QualityExt(QualityMP3_320) != ".mp3" {
		t.Errorf("expected .mp3, got %s", QualityExt(QualityMP3_320))
	}

	fbFLAC := FallbackQualities(QualityFLAC)
	if len(fbFLAC) != 2 || fbFLAC[0] != QualityMP3_320 || fbFLAC[1] != QualityMP3_128 {
		t.Errorf("unexpected FLAC fallbacks: %v", fbFLAC)
	}

	fb320 := FallbackQualities(QualityMP3_320)
	if len(fb320) != 1 || fb320[0] != QualityMP3_128 {
		t.Errorf("unexpected MP3_320 fallbacks: %v", fb320)
	}
}
