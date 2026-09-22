package server

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"music-server/internal/config"
)

// The Android app, handed out by the server that it talks to.
//
// There is no store to publish a self-hosted client to, so the APKs are
// simply files in a directory: drop Music-1.2.0.apk in, and it appears in
// the app under "Android app". Nothing is built or generated here — the
// server only lists the directory and serves what is in it, which is why
// the file name pattern is strict: it is also where the version shown to
// people comes from.
//
// See `./gradlew :androidApp:releaseApk` in mobile/, which produces a file
// with exactly this name.

// apkNameRe matches the only file name this serves, and captures the
// version. Keeping it strict is also what makes serving by name safe: no
// separators, no dots beyond the version, nothing to traverse with.
var apkNameRe = regexp.MustCompile(`^Music-([0-9A-Za-z._+-]{1,40})\.apk$`)

// AppRelease is one APK in the directory.
type AppRelease struct {
	Version  string `json:"version"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	// ModifiedAt is when the file was put there, RFC 3339.
	ModifiedAt string `json:"modifiedAt"`
}

// apkDir is where the APKs live: MUSIC_APK_DIR, or <dataDir>/apk.
func (s *Server) apkDir() string {
	if dir := config.Value("MUSIC_APK_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(s.dataDir, "apk")
}

// handleAppReleases: GET /api/app/releases — what's in the directory,
// newest version first. An empty list (no directory, nothing in it) is a
// normal answer: the clients then simply don't offer the download.
func (s *Server) handleAppReleases(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(s.apkDir())
	out := []AppRelease{}
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			m := apkNameRe.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			rel := AppRelease{Version: m[1], FileName: e.Name()}
			if info, err := e.Info(); err == nil {
				rel.Size = info.Size()
				rel.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339)
			}
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i].Version, out[j].Version) > 0 })
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// handleAppReleaseDownload: GET /api/app/releases/{file} — the APK itself.
// A media route, because it is reached by a plain link from the browser or
// a phone, which can't set an Authorization header.
func (s *Server) handleAppReleaseDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !apkNameRe.MatchString(name) {
		writeErr(w, http.StatusNotFound, "no such build")
		return
	}
	f, err := os.Open(filepath.Join(s.apkDir(), name))
	if err != nil {
		writeErr(w, http.StatusNotFound, "no such build")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeErr(w, http.StatusNotFound, "no such build")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// compareVersions orders dotted versions the way people read them: 1.10.0
// after 1.9.0, 1.2.1 after 1.2, and a release after its own pre-releases
// ("1.2.0" newer than "1.2.0-beta"). Returns -1, 0 or 1.
func compareVersions(a, b string) int {
	as, bs := strings.FieldsFunc(a, isVersionSep), strings.FieldsFunc(b, isVersionSep)
	for i := 0; i < len(as) || i < len(bs); i++ {
		// One of them ran out of parts. A further number means the longer
		// one is newer (1.2.1 after 1.2); a further word is a pre-release
		// suffix, which makes it older (1.2.0 after 1.2.0-beta).
		if i >= len(as) {
			return -laterByExtraPart(bs[i])
		}
		if i >= len(bs) {
			return laterByExtraPart(as[i])
		}
		x, y := as[i], bs[i]
		xi, xErr := strconv.Atoi(x)
		yi, yErr := strconv.Atoi(y)
		switch {
		case xErr == nil && yErr == nil:
			if xi != yi {
				return sign(xi - yi)
			}
		case xErr == nil:
			return 1 // a number outranks a word in the same position
		case yErr == nil:
			return -1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return 0
}

// laterByExtraPart: 1 when having this extra part makes a version newer.
func laterByExtraPart(part string) int {
	if _, err := strconv.Atoi(part); err == nil {
		return 1
	}
	return -1
}

func isVersionSep(r rune) bool { return r == '.' || r == '-' || r == '_' || r == '+' }

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
