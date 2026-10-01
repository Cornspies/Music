package logfile

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	KeepDays  = 14
	prefix    = "music-server-"
	suffix    = ".log"
	dayFormat = "2006-01-02"
)

type Writer struct {
	dir     string
	console io.Writer
	mu      sync.Mutex
	day     string
	file    *os.File
}

func Open(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir}
	if isTerminal(os.Stderr) {
		w.console = os.Stderr
	}
	if err := w.openDay(time.Now().Format(dayFormat)); err != nil {
		return nil, err
	}
	go w.pruneDaily()
	return w, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.console != nil {
		_, _ = w.console.Write(p)
	}
	if day := time.Now().Format(dayFormat); day != w.day {
		if err := w.openDayLocked(day); err != nil && w.file == nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

func (w *Writer) openDay(day string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.openDayLocked(day)
}

func (w *Writer) openDayLocked(day string) error {
	f, err := os.OpenFile(filepath.Join(w.dir, prefix+day+suffix), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	w.file, w.day = f, day
	return nil
}

func (w *Writer) pruneDaily() {
	for {
		Prune(w.dir, time.Now())
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 1, 0, now.Location())
		time.Sleep(min(time.Until(next), time.Hour))
	}
}

func Prune(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("logs: read %s: %v", dir, err)
		return
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	oldest := today.AddDate(0, 0, -(KeepDays - 1))
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		day, err := time.ParseInLocation(dayFormat, strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix), now.Location())
		if err != nil || !day.Before(oldest) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			log.Printf("logs: remove %s: %v", name, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		log.Printf("logs: removed %d log files older than %d days", removed, KeepDays)
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
