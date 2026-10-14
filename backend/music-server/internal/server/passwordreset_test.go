package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testClient struct {
	t   *testing.T
	srv *httptest.Server
}

func newTestClient(t *testing.T) (*testClient, *Server) {
	t.Helper()
	s, err := New(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.putSetting(settingRegistrationOpen, "1"); err != nil {
		t.Fatal(err)
	}
	authLimiter = &limiter{entries: map[string]*attemptRecord{}}
	accountLimiter = &limiter{entries: map[string]*attemptRecord{}}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(func() {
		ts.Close()
		s.db.Close()
	})
	return &testClient{t: t, srv: ts}, s
}

func (c *testClient) do(method, path, token string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case multipartBody:
		buf.Write(b.data)
		contentType = b.contentType
	default:
		_ = json.NewEncoder(&buf).Encode(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, &buf)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (c *testClient) register(username, password string) (token, id string) {
	c.t.Helper()
	code, out := c.do("POST", "/api/auth/register", "", map[string]string{"username": username, "password": password})
	if code != http.StatusCreated {
		c.t.Fatalf("register %s: %d %v", username, code, out)
	}
	return out["token"].(string), out["user"].(map[string]any)["id"].(string)
}

type multipartBody struct {
	data        []byte
	contentType string
}

func imageUpload(t *testing.T, w, h int) multipartBody {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "cover.png")
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	return multipartBody{data: buf.Bytes(), contentType: mw.FormDataContentType()}
}

func TestPasswordResetLink(t *testing.T) {
	c, _ := newTestClient(t)
	adminToken, _ := c.register("admin", "admin-password")
	userToken, userID := c.register("sam", "old-password")

	if code, _ := c.do("POST", "/api/admin/users/"+userID+"/password-reset", userToken, nil); code != http.StatusForbidden {
		t.Fatalf("non-admin created a reset link: %d", code)
	}
	code, reset := c.do("POST", "/api/admin/users/"+userID+"/password-reset", adminToken, nil)
	if code != http.StatusCreated {
		t.Fatalf("create reset: %d %v", code, reset)
	}
	resetCode := reset["code"].(string)

	if _, check := c.do("GET", "/api/auth/reset/"+resetCode, "", nil); check["valid"] != true || check["username"] != "sam" {
		t.Fatalf("check reset: %v", check)
	}
	if code, _ := c.do("POST", "/api/auth/reset", "", map[string]string{"code": resetCode, "password": "short"}); code != http.StatusBadRequest {
		t.Fatalf("short password accepted: %d", code)
	}
	code, out := c.do("POST", "/api/auth/reset", "", map[string]string{"code": resetCode, "password": "new-password"})
	if code != http.StatusOK || out["token"] == nil {
		t.Fatalf("reset: %d %v", code, out)
	}

	if code, _ := c.do("GET", "/api/auth/me", userToken, nil); code != http.StatusUnauthorized {
		t.Fatalf("old session still works after reset: %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", "", map[string]string{"username": "sam", "password": "old-password"}); code != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", "", map[string]string{"username": "sam", "password": "new-password"}); code != http.StatusOK {
		t.Fatalf("new password rejected: %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/reset", "", map[string]string{"code": resetCode, "password": "another-password"}); code != http.StatusForbidden {
		t.Fatalf("reset link worked twice: %d", code)
	}
	if _, check := c.do("GET", "/api/auth/reset/"+resetCode, "", nil); check["valid"] != false {
		t.Fatalf("used link still valid: %v", check)
	}
}

func TestNewPasswordResetReplacesOld(t *testing.T) {
	c, s := newTestClient(t)
	c.register("admin", "admin-password")
	first, err := s.CreatePasswordResetFor("Admin")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreatePasswordResetFor("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, check := c.do("GET", "/api/auth/reset/"+first.Code, "", nil); check["valid"] != false {
		t.Fatal("an older reset link still works after a new one was made")
	}
	if _, check := c.do("GET", "/api/auth/reset/"+second.Code, "", nil); check["valid"] != true {
		t.Fatal("the newest reset link was rejected")
	}
	if _, err := s.CreatePasswordResetFor("nobody"); err == nil {
		t.Fatal("made a reset link for an unknown user")
	}
}
