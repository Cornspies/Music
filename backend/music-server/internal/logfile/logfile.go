package logfile

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	KeepDays    = 14
	prefix      = "music-server-"
	errorPrefix = "music-server-errors-"
	suffix      = ".log"
	dayFormat   = "2006-01-02"
)

// Entries whose message starts with one of these also go to the error log.
var errorMarkers = [][]byte{[]byte("ERROR "), []byte("PANIC "), []byte("FATAL ")}

type Writer struct {
	dir     string
	console io.Writer
	mu      sync.Mutex
	all     daily
	errors  daily
}

type daily struct {
	prefix string
	day    string
	file   *os.File
}

func Open(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, all: daily{prefix: prefix}, errors: daily{prefix: errorPrefix}}
	if isTerminal(os.Stderr) {
		w.console = os.Stderr
	}
	w.mu.Lock()
	err := w.all.open(dir, time.Now().Format(dayFormat))
	w.mu.Unlock()
	if err != nil {
		return nil, err
	}
	go w.pruneDaily()
	return w, nil
}

// ErrorFile is the name pattern of the error log, for the start-up message.
func ErrorFile() string { return errorPrefix + "<date>" + suffix }

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.console != nil {
		_, _ = w.console.Write(p)
	}
	day := time.Now().Format(dayFormat)
	if isError(p) {
		// The error log is created on the first error of the day, so a quiet
		// day leaves no empty file behind.
		if w.errors.day != day {
			_ = w.errors.open(w.dir, day)
		}
		if w.errors.file != nil {
			_, _ = w.errors.file.Write(p)
		}
	}
	if day != w.all.day {
		if err := w.all.open(w.dir, day); err != nil && w.all.file == nil {
			return 0, err
		}
	}
	return w.all.file.Write(p)
}

// isError looks past the standard log timestamp ("2006/01/02 15:04:05 ") for
// an error marker at the start of the message.
func isError(p []byte) bool {
	msg := p
	if len(msg) > 20 && msg[4] == '/' && msg[7] == '/' && msg[10] == ' ' && msg[13] == ':' {
		msg = msg[20:]
	}
	for _, m := range errorMarkers {
		if bytes.HasPrefix(msg, m) {
			return true
		}
	}
	return false
}

func (d *daily) open(dir, day string) error {
	f, err := os.OpenFile(filepath.Join(dir, d.prefix+day+suffix), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	if d.file != nil {
		_ = d.file.Close()
	}
	d.file, d.day = f, day
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
		log.Printf("ERROR logs: read %s: %v", dir, err)
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
		stamp := strings.TrimSuffix(name, suffix)
		if strings.HasPrefix(stamp, errorPrefix) {
			stamp = strings.TrimPrefix(stamp, errorPrefix)
		} else {
			stamp = strings.TrimPrefix(stamp, prefix)
		}
		day, err := time.ParseInLocation(dayFormat, stamp, now.Location())
		if err != nil || !day.Before(oldest) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			log.Printf("ERROR logs: remove %s: %v", name, err)
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
