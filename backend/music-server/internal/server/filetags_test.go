package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// taggedMP3 builds an MP3 with an ID3v2.3 tag, a cover picture and a few
// silent frames.
func taggedMP3(tags map[string]string, picture []byte) []byte {
	var body bytes.Buffer
	frame := func(id string, data []byte) {
		body.WriteString(id)
		_ = binary.Write(&body, binary.BigEndian, uint32(len(data)))
		body.Write([]byte{0, 0})
		body.Write(data)
	}
	for _, id := range []string{"TIT2", "TPE1", "TALB", "TPE2", "TCON", "TYER", "TRCK"} {
		if text, ok := tags[id]; ok {
			frame(id, append([]byte{0}, text...))
		}
	}
	if picture != nil {
		frame("APIC", append([]byte("\x00image/png\x00\x03\x00"), picture...))
	}
	n := body.Len()
	out := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	out = append(out, body.Bytes()...)
	audio := make([]byte, 417)
	copy(audio, []byte{0xff, 0xfb, 0x90, 0x00})
	return append(out, bytes.Repeat(audio, 20)...)
}

func TestTextTagsMatchTheFullRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	picture := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 500)
	data := taggedMP3(map[string]string{
		"TIT2": " Shape of You ", "TPE1": "Ed Sheeran", "TALB": "Divide", "TPE2": "Various Artists",
		"TCON": "Pop", "TYER": "2017", "TRCK": "4/16",
	}, picture)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	tags := readTextTags(path)
	want := fileMetadata{title: "Shape of You", artist: "Ed Sheeran", album: "Divide", albumArtist: "Various Artists", genre: "Pop", year: 2017, trackNo: 4}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("text tags: %+v, want %+v", tags, want)
	}
	full := readMetadata(path)
	if full.duration <= 0 || !bytes.Equal(full.picture, picture) {
		t.Errorf("the full read lost the duration (%v) or the picture (%d bytes)", full.duration, len(full.picture))
	}
	full.duration, full.picture = 0, nil
	if !reflect.DeepEqual(full, tags) {
		t.Errorf("full read %+v, text tags %+v", full, tags)
	}

	if got := readTextTags(filepath.Join(t.TempDir(), "missing.mp3")); !reflect.DeepEqual(got, fileMetadata{}) {
		t.Errorf("a missing file gave %+v", got)
	}
}

func TestLibraryFileTagsListsDifferencesAndStopsWhenCancelled(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	data := taggedMP3(map[string]string{"TIT2": "Shape of You (Live)", "TPE1": "Ed Sheeran", "TYER": "2017"}, []byte("cover"))
	if err := os.WriteFile(filepath.Join(s.dataDir, "uploads", "shape.mp3"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	call := func(ctx context.Context) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/library/file-tags", nil).WithContext(ctx)
		s.handleLibraryFileTags(rec, withUser(req, &User{ID: userID, Role: roleUser}))
		return rec
	}

	var diffs []struct {
		Track   Track       `json:"track"`
		Changes []TagChange `json:"changes"`
	}
	if err := json.Unmarshal(call(context.Background()).Body.Bytes(), &diffs); err != nil {
		t.Fatal(err)
	}
	want := []TagChange{{Field: "title", Current: "Shape of You", File: "Shape of You (Live)"}, {Field: "year", Current: "", File: "2017"}}
	if len(diffs) != 1 || diffs[0].Track.ID != "shape" || !reflect.DeepEqual(diffs[0].Changes, want) {
		t.Errorf("differences: %+v, want %+v for the one tagged file", diffs, want)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if rec := call(cancelled); rec.Body.Len() != 0 {
		t.Errorf("a cancelled request still got an answer: %s", rec.Body.String())
	}
}
