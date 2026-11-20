package server

import (
	"bytes"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogHidesCodesAndControlCharacters(t *testing.T) {
	_, s := newTestClient(t)
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	routes := s.Routes()
	for _, target := range []string{
		"/api/auth/reset/s3cret-reset-code",
		"/api/auth/invite/s3cret-invite-code",
		"/api/share/s3cret-share-token/cover",
		"/api/auth/config",
		"/api/nothing%0AGET%20/forged%20-%3E%20200",
	} {
		routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}

	out := buf.String()
	if strings.Contains(out, "s3cret") {
		t.Fatalf("a code or token was logged:\n%s", out)
	}
	for _, want := range []string{"GET /api/auth/reset/{code} ->", "GET /api/share/{token}/cover ->", "GET /api/auth/config ->"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log has no %q:\n%s", want, out)
		}
	}
	if lines := strings.Count(out, "\n"); lines != 5 {
		t.Fatalf("5 requests wrote %d log lines:\n%s", lines, out)
	}
}
