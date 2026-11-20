package server

import (
	"net/http"
	"testing"
)

func TestInviteStaysUsedAfterAccountIsDeleted(t *testing.T) {
	c, s := newTestClient(t)
	adminToken, _ := c.register("admin", "admin-password")
	code, inv := c.do("POST", "/api/admin/invites", adminToken, map[string]string{})
	if code != http.StatusCreated {
		t.Fatalf("create invite: %d %v", code, inv)
	}
	link := inv["code"].(string)
	if err := s.putSetting(settingRegistrationOpen, "0"); err != nil {
		t.Fatal(err)
	}

	join := func(name string) (int, map[string]any) {
		return c.do("POST", "/api/auth/register", "",
			map[string]string{"username": name, "password": "guest-password", "inviteCode": link})
	}
	code, out := join("guest")
	if code != http.StatusCreated {
		t.Fatalf("register with invite: %d %v", code, out)
	}
	guestToken := out["token"].(string)
	if code, _ := c.do("POST", "/api/auth/delete-account", guestToken, map[string]string{"password": "guest-password"}); code != http.StatusNoContent {
		t.Fatalf("delete account: %d", code)
	}

	if _, check := c.do("GET", "/api/auth/invite/"+link, "", nil); check["valid"] != false || check["status"] != "used" {
		t.Fatalf("invite after its account was deleted: %v", check)
	}
	if code, out := join("guest2"); code != http.StatusForbidden {
		t.Fatalf("used invite accepted again: %d %v", code, out)
	}
	if code, _ := c.do("POST", "/api/admin/invites/"+link+"/revoke", adminToken, nil); code == http.StatusOK {
		_, list := c.do("GET", "/api/auth/invite/"+link, "", nil)
		if list["status"] != "used" {
			t.Fatalf("a used invite was revoked: %v", list)
		}
	}
}
