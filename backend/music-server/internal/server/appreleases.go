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

type releaseKind struct {
	name        *regexp.Regexp
	contentType string
}

var releaseKinds = map[string]releaseKind{
	"android":  {regexp.MustCompile(`^Music-([0-9A-Za-z._+-]{1,40})\.apk$`), "application/vnd.android.package-archive"},
	"appimage": {regexp.MustCompile(`^Music_([0-9A-Za-z.+-]{1,40})_(?:amd64|x86_64)\.AppImage$`), "application/octet-stream"},
	"deb":      {regexp.MustCompile(`^Music_([0-9A-Za-z.+-]{1,40})_amd64\.deb$`), "application/vnd.debian.binary-package"},
	"rpm":      {regexp.MustCompile(`^Music-([0-9A-Za-z.+]{1,40})-[0-9]+\.x86_64\.rpm$`), "application/x-rpm"},
	"windows":  {regexp.MustCompile(`^Music_([0-9A-Za-z.+-]{1,40})_x64-setup\.exe$`), "application/vnd.microsoft.portable-executable"},
	"msi":      {regexp.MustCompile(`^Music_([0-9A-Za-z.+-]{1,40})_x64_[A-Za-z-]{2,10}\.msi$`), "application/x-msi"},
}

func requestedKind(w http.ResponseWriter, r *http.Request) (releaseKind, bool) {
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	if platform == "" {
		platform = "android"
	}
	kind, ok := releaseKinds[platform]
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown platform")
	}
	return kind, ok
}

type AppRelease struct {
	Version    string `json:"version"`
	FileName   string `json:"fileName"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

func (s *Server) apkDir() string {
	if dir := config.Value("MUSIC_APK_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(s.dataDir, "apk")
}

func (s *Server) handleAppReleases(w http.ResponseWriter, r *http.Request) {
	kind, ok := requestedKind(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.appReleases(kind))
}

func (s *Server) handleLatestAppRelease(w http.ResponseWriter, r *http.Request) {
	kind, ok := requestedKind(w, r)
	if !ok {
		return
	}
	current := strings.TrimSpace(r.URL.Query().Get("current"))
	var out struct {
		Latest          *AppRelease `json:"latest"`
		UpdateAvailable bool        `json:"updateAvailable"`
	}
	if releases := s.appReleases(kind); len(releases) > 0 {
		out.Latest = &releases[0]
		out.UpdateAvailable = current != "" && compareVersions(releases[0].Version, current) > 0
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) appReleases(kind releaseKind) []AppRelease {
	entries, err := os.ReadDir(s.apkDir())
	out := []AppRelease{}
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			m := kind.name.FindStringSubmatch(e.Name())
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
	return out
}

func (s *Server) handleAppReleaseDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	contentType := ""
	for _, kind := range releaseKinds {
		if kind.name.MatchString(name) {
			contentType = kind.contentType
			break
		}
	}
	if contentType == "" {
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
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func compareVersions(a, b string) int {
	as, bs := strings.FieldsFunc(a, isVersionSep), strings.FieldsFunc(b, isVersionSep)
	for i := 0; i < len(as) || i < len(bs); i++ {
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
			return 1
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
