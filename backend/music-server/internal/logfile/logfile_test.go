package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestErrorsGoToBothLogs(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"2026/10/24 12:00:00 GET /api/tracks -> 200 (3ms)\n",
		"2026/10/24 12:00:01 ERROR record play: disk full\n",
		"2026/10/24 12:00:02 PANIC in import (recovered): boom\nstack line\n",
		"2026/10/24 12:00:03 metadata lookup (MusicBrainz): ERROR in the middle is not an error\n",
	}
	for _, l := range lines {
		if _, err := w.Write([]byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Now().Format(dayFormat)
	all, _ := os.ReadFile(filepath.Join(dir, prefix+day+suffix))
	errs, _ := os.ReadFile(filepath.Join(dir, errorPrefix+day+suffix))
	if got := string(all); got != strings.Join(lines, "") {
		t.Fatalf("main log = %q", got)
	}
	if got, want := string(errs), lines[1]+lines[2]; got != want {
		t.Fatalf("error log = %q, want %q", got, want)
	}
}

func TestPruneRemovesOldErrorLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)
	names := []string{
		"music-server-2026-10-01.log", "music-server-errors-2026-10-01.log",
		"music-server-2026-10-24.log", "music-server-errors-2026-10-24.log",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	Prune(dir, now)
	for i, n := range names {
		_, err := os.Stat(filepath.Join(dir, n))
		if kept := err == nil; kept != (i >= 2) {
			t.Fatalf("%s kept = %v", n, kept)
		}
	}
}
