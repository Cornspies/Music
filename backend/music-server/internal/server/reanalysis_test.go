package server

import (
	"net/http"
	"testing"
)

func TestAdminTrimRule(t *testing.T) {
	c, s := newTestClient(t)
	adminToken, _ := c.register("admin", "admin-password")
	userToken, _ := c.register("listener", "listener-password")

	if got := s.trimRule(); got != defaultTrimRule {
		t.Fatalf("rule before any change = %+v", got)
	}
	if code, _ := c.do("GET", "/api/admin/analysis", userToken, nil); code != http.StatusForbidden {
		t.Fatalf("non-admin GET = %d", code)
	}

	bad := []trimRule{
		{ThresholdDB: -10, MinSeconds: 0.5, MarginSeconds: 0.05},
		{ThresholdDB: -55, MinSeconds: 0.05, MarginSeconds: 0},
		{ThresholdDB: -55, MinSeconds: 0.5, MarginSeconds: 0.5},
	}
	for _, r := range bad {
		if code, _ := c.do("POST", "/api/admin/analysis", adminToken, map[string]any{"rule": r}); code != http.StatusBadRequest {
			t.Fatalf("rule %+v accepted: %d", r, code)
		}
	}

	want := trimRule{ThresholdDB: -60, MinSeconds: 1, MarginSeconds: 0.1}
	code, out := c.do("POST", "/api/admin/analysis", adminToken, map[string]any{"rule": want})
	if code != http.StatusOK {
		t.Fatalf("save rule = %d %v", code, out)
	}
	if got := s.trimRule(); got != want {
		t.Fatalf("saved rule = %+v, want %+v", got, want)
	}
	if got := want.String(); got != "-60dB for 1s" {
		t.Fatalf("String() = %q", got)
	}

	if err := s.putSetting(settingTrimRule, `{"thresholdDb":5}`); err != nil {
		t.Fatal(err)
	}
	if got := s.trimRule(); got != defaultTrimRule {
		t.Fatalf("invalid stored rule = %+v, want the default", got)
	}
}
