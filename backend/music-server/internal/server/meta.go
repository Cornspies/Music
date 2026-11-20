package server

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	mp3dec "github.com/tcolgate/mp3"
)

type fileMetadata struct {
	title       string
	artist      string
	album       string
	albumArtist string
	genre       string
	year        int
	trackNo     int
	duration    float64
	picture     []byte
}

func readMetadata(path string) fileMetadata {
	meta, m := readTags(path)
	if m != nil {
		if p := m.Picture(); p != nil && len(p.Data) > 0 {
			meta.picture = p.Data
		}
	}

	if strings.EqualFold(filepath.Ext(path), ".mp3") {
		meta.duration = mp3DurationSeconds(path)
	}

	return meta
}

// readTextTags is readMetadata without the picture and without the duration,
// which for an MP3 means walking every frame of the file.
func readTextTags(path string) fileMetadata {
	meta, _ := readTags(path)
	return meta
}

func readTags(path string) (fileMetadata, tag.Metadata) {
	var meta fileMetadata

	f, err := os.Open(path)
	if err != nil {
		return meta, nil
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil || m == nil {
		return meta, nil
	}
	meta.title = strings.TrimSpace(m.Title())
	meta.artist = strings.TrimSpace(m.Artist())
	meta.album = strings.TrimSpace(m.Album())
	meta.albumArtist = strings.TrimSpace(m.AlbumArtist())
	meta.genre = strings.TrimSpace(m.Genre())
	meta.year = m.Year()
	track, _ := m.Track()
	meta.trackNo = track
	return meta, m
}

func mp3DurationSeconds(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	dec := mp3dec.NewDecoder(f)
	var totalNS int64
	for {
		var frame mp3dec.Frame
		skipped := 0
		if err := dec.Decode(&frame, &skipped); err != nil {
			break
		}
		totalNS += frame.Duration().Nanoseconds()
	}
	sec := float64(totalNS) / 1e9
	if sec > 0 && sec < 100000 {
		return sec
	}
	return 0
}

func albumArtistFor(albumArtist, artist string) string {
	albumArtist = strings.TrimSpace(albumArtist)
	if strings.EqualFold(albumArtist, strings.TrimSpace(artist)) {
		return ""
	}
	return albumArtist
}
