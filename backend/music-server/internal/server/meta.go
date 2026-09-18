package server

import (
        "os"
        "path/filepath"
        "strings"

        "github.com/dhowden/tag"
        mp3dec "github.com/tcolgate/mp3"
)

type fileMetadata struct {
        title   string
        artist  string
        album   string
        genre   string
        year    int
        trackNo int
        duration float64
        picture []byte
}

// readMetadata extracts ID3/Vorbis/MP4 tags, embedded cover art and (for MP3)
// the play duration from an audio file. Fields that have no tag are left as
// their zero value — callers (see enrichMissingMetadata) rely on that to
// tell a genuinely missing tag apart from one that's merely empty text.
func readMetadata(path string) fileMetadata {
        var meta fileMetadata

        if f, err := os.Open(path); err == nil {
                if m, err := tag.ReadFrom(f); err == nil && m != nil {
                        meta.title = strings.TrimSpace(m.Title())
                        meta.artist = strings.TrimSpace(m.Artist())
                        meta.album = strings.TrimSpace(m.Album())
                        meta.genre = strings.TrimSpace(m.Genre())
                        meta.year = m.Year()
                        track, _ := m.Track()
                        meta.trackNo = track
                        if p := m.Picture(); p != nil && len(p.Data) > 0 {
                                meta.picture = p.Data
                        }
                }
                f.Close()
        }

        if strings.EqualFold(filepath.Ext(path), ".mp3") {
                meta.duration = mp3DurationSeconds(path)
        }

        return meta
}

// mp3DurationSeconds sums frame durations of an MP3 file.
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
                        break // io.EOF or corrupt frame: stop counting
                }
                totalNS += frame.Duration().Nanoseconds()
        }
        sec := float64(totalNS) / 1e9
        if sec > 0 && sec < 100000 {
                return sec
        }
        return 0
}
