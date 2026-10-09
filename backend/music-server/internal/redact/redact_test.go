package redact

import (
	"bytes"
	"testing"
)

func TestRedact(t *testing.T) {
	Register("short", "  0123456789abcdefARL  ", "0123456789abcdefARL")
	if got := String("cookie arl=0123456789abcdefARL; x"); got != "cookie arl=[redacted]; x" {
		t.Fatalf("String = %q", got)
	}
	if got := String("short stays"); got != "short stays" {
		t.Fatalf("short value was redacted: %q", got)
	}
	var buf bytes.Buffer
	w := Writer(&buf)
	in := []byte(`{"error":"Get https://x/?arl=0123456789abcdefARL failed"}`)
	n, err := w.Write(in)
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if bytes.Contains(buf.Bytes(), []byte("0123456789abcdefARL")) {
		t.Fatalf("secret leaked: %s", buf.Bytes())
	}
}
