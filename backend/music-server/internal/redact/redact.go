package redact

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

const (
	minSecretLen = 8
	mask         = "[redacted]"
)

var (
	mu      sync.RWMutex
	secrets [][]byte
)

func Register(values ...string) {
	mu.Lock()
	defer mu.Unlock()
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) < minSecretLen {
			continue
		}
		dup := false
		for _, s := range secrets {
			if string(s) == v {
				dup = true
				break
			}
		}
		if !dup {
			secrets = append(secrets, []byte(v))
		}
	}
}

func Bytes(b []byte) []byte {
	mu.RLock()
	defer mu.RUnlock()
	for _, s := range secrets {
		if bytes.Contains(b, s) {
			b = bytes.ReplaceAll(b, s, []byte(mask))
		}
	}
	return b
}

func String(s string) string {
	mu.RLock()
	defer mu.RUnlock()
	for _, secret := range secrets {
		s = strings.ReplaceAll(s, string(secret), mask)
	}
	return s
}

type writer struct{ w io.Writer }

func (r writer) Write(p []byte) (int, error) {
	if _, err := r.w.Write(Bytes(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func Writer(w io.Writer) io.Writer { return writer{w} }
