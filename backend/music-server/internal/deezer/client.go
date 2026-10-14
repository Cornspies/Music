package deezer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	gwURL         = "https://www.deezer.com/ajax/gw-light.php"
	legacyAPIURL  = "https://api.deezer.com"
	mediaAPIURL   = "https://media.deezer.com/v1/get_url"
	userAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	maxTrackBytes = 1024 * 1024 * 1024

	relogInterval = 30 * time.Second
	maxRedirects  = 10
)

var downloadIdleTimeout = 30 * time.Second

func IsAllowedDeezerURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "deezer.com" || strings.HasSuffix(host, ".deezer.com") ||
		host == "dzcdn.net" || strings.HasSuffix(host, ".dzcdn.net")
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if !IsAllowedDeezerURL(req.URL.String()) {
		return fmt.Errorf("redirect to disallowed host %q", req.URL.Hostname())
	}
	return nil
}

func newHTTPClient(jar http.CookieJar) *http.Client {
	return &http.Client{
		Jar:           jar,
		Timeout:       45 * time.Second,
		CheckRedirect: checkRedirect,
	}
}

type Client struct {
	http             *http.Client
	arl              string
	token            string
	licenseToken     string
	user             *UserInfo
	preferredQuality string
	mu               sync.RWMutex

	loginMu   sync.Mutex
	lastLogin time.Time
}

func NewClient(arl string, preferredQuality string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}

	trimmedARL := strings.TrimSpace(arl)
	if trimmedARL != "" {
		deezerURL, _ := url.Parse("https://www.deezer.com")
		jar.SetCookies(deezerURL, []*http.Cookie{
			{
				Name:   "arl",
				Value:  trimmedARL,
				Domain: ".deezer.com",
				Path:   "/",
			},
		})
	}

	q := normalizeQuality(preferredQuality, trimmedARL != "")

	c := &Client{
		http:             newHTTPClient(jar),
		arl:              trimmedARL,
		preferredQuality: q,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := c.login(ctx); err != nil {
		if trimmedARL != "" {
			return nil, fmt.Errorf("deezer login failed: %w", err)
		}
	}

	return c, nil
}

func normalizeQuality(requested string, hasARL bool) string {
	q := strings.ToUpper(strings.TrimSpace(requested))
	if IsKnownQuality(q) {
		return q
	}
	if hasARL {
		return QualityMP3_320
	}
	return QualityMP3_128
}

func (c *Client) User() *UserInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

func (c *Client) PreferredQuality() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.preferredQuality
}

func (c *Client) HasARL() bool {
	return c.arl != ""
}

func (c *Client) newRequest(ctx context.Context, method, targetURL string, body io.Reader) (*http.Request, error) {
	if !IsAllowedDeezerURL(targetURL) {
		return nil, fmt.Errorf("disallowed Deezer URL: %s", targetURL)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Connection", "keep-alive")
	return req, nil
}

func (c *Client) login(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	return c.loginLocked(ctx)
}

func (c *Client) currentLicense() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.licenseToken
}

func (c *Client) refreshLicense(ctx context.Context, stale string) string {
	if !c.HasARL() {
		return c.currentLicense()
	}

	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	if cur := c.currentLicense(); cur != stale {
		return cur
	}
	if time.Since(c.lastLogin) < relogInterval {
		return stale
	}
	if err := c.loginLocked(ctx); err != nil {
		return stale
	}
	return c.currentLicense()
}

func (c *Client) loginLocked(ctx context.Context) error {
	c.lastLogin = time.Now()

	initReq, err := c.newRequest(ctx, http.MethodGet, gwURL, nil)
	if err != nil {
		return err
	}
	initResp, err := c.http.Do(initReq)
	if err != nil {
		return fmt.Errorf("session init: %w", err)
	}
	_ = initResp.Body.Close()

	userDataRaw, err := c.rawAPICall(ctx, "deezer.getUserData", "null", map[string]any{})
	if err != nil {
		return fmt.Errorf("get user data: %w", err)
	}

	var parsed struct {
		Results struct {
			CheckForm string `json:"checkForm"`
			OfferName string `json:"OFFER_NAME"`
			User      struct {
				UserID      any    `json:"USER_ID"`
				BlogName    string `json:"BLOG_NAME"`
				UserPicture string `json:"USER_PICTURE"`
				OfferName   string `json:"OFFER_NAME"`
				Options     struct {
					LicenseToken string `json:"license_token"`
					OfferName    string `json:"offer_name"`
					AdsAudio     bool   `json:"ads_audio"`
					AdsDisplay   bool   `json:"ads_display"`
				} `json:"OPTIONS"`
			} `json:"USER"`
		} `json:"results"`
	}

	if err := json.Unmarshal(userDataRaw, &parsed); err != nil {
		return fmt.Errorf("parse user data: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.token = parsed.Results.CheckForm
	c.licenseToken = parsed.Results.User.Options.LicenseToken

	var uid uint64
	switch v := parsed.Results.User.UserID.(type) {
	case float64:
		uid = uint64(v)
	case string:
		uid, _ = strconv.ParseUint(v, 10, 64)
	case int64:
		uid = uint64(v)
	}

	if uid == 0 {
		c.user = nil
	} else {
		var img string
		pic := parsed.Results.User.UserPicture
		if pic != "" && !isAllZeros(pic) {
			img = fmt.Sprintf("https://e-cdns-images.dzcdn.net/images/user/%s/250x250-000000-80-0-0.jpg", pic)
		}

		offer := strings.ToLower(firstNonEmpty(
			parsed.Results.OfferName,
			parsed.Results.User.OfferName,
			parsed.Results.User.Options.OfferName,
		))
		hasAds := parsed.Results.User.Options.AdsAudio || parsed.Results.User.Options.AdsDisplay
		isFree := strings.Contains(offer, "free") || strings.Contains(offer, "gratuit") ||
			strings.Contains(offer, "kostenlos") || (offer == "" && hasAds)

		c.user = &UserInfo{
			ID:            uid,
			Name:          firstNonEmpty(parsed.Results.User.BlogName, "Unknown"),
			Image:         img,
			IsFreeAccount: isFree,
		}

		if isFree {
			c.preferredQuality = QualityMP3_128
		}
	}

	return nil
}

func isAllZeros(s string) bool {
	for _, c := range s {
		if c != '0' {
			return false
		}
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func (c *Client) rawAPICall(ctx context.Context, method, token string, params any) ([]byte, error) {
	bodyBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	q := url.Values{
		"api_version": {"1.0"},
		"api_token":   {token},
		"input":       {"3"},
		"method":      {method},
	}
	targetURL := gwURL + "?" + q.Encode()

	req, err := c.newRequest(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gw-light status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}

	var errCheck struct {
		Error map[string]any `json:"error"`
	}
	if json.Unmarshal(data, &errCheck) == nil && len(errCheck.Error) > 0 {
		for _, v := range errCheck.Error {
			if str, ok := v.(string); ok && str != "" {
				return nil, fmt.Errorf("deezer error: %s", str)
			}
		}
		return nil, errors.New("deezer error")
	}

	return data, nil
}

func (c *Client) APICall(ctx context.Context, method string, params any) ([]byte, error) {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	if token == "" {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		c.mu.RLock()
		token = c.token
		c.mu.RUnlock()
	}

	data, err := c.rawAPICall(ctx, method, token, params)
	if err != nil && (strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "auth") || strings.Contains(err.Error(), "csrf")) {
		if rerr := c.login(ctx); rerr == nil {
			c.mu.RLock()
			token = c.token
			c.mu.RUnlock()
			return c.rawAPICall(ctx, method, token, params)
		}
	}
	return data, err
}

func (c *Client) GetTrackData(ctx context.Context, trackID string) (map[string]any, error) {
	raw, err := c.APICall(ctx, "song.getData", map[string]any{"SNG_ID": trackID})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results map[string]any `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse song.getData: %w", err)
	}
	if len(resp.Results) == 0 {
		return nil, errors.New("song not found")
	}

	data := resp.Results
	if inner, ok := resp.Results["DATA"].(map[string]any); ok && len(inner) > 0 {
		data = inner
	}

	if fb, ok := resp.Results["FALLBACK"].(map[string]any); ok && len(fb) > 0 {
		if _, exists := data["FALLBACK"]; !exists {
			data["FALLBACK"] = fb
		}
	} else if fbID := extractVal(resp.Results["FALLBACK"]); fbID != "" && fbID != "0" {
		if _, exists := data["FALLBACK"]; !exists {
			data["FALLBACK"] = fbID
		}
	}

	return data, nil
}

func (c *Client) GetTrackDownloadURL(ctx context.Context, trackData map[string]any, quality string, fallback bool) (string, string, error) {
	u, q, err := c.resolveTrackURL(ctx, trackData, quality, fallback)
	if err == nil {
		return u, q, nil
	}

	if fbData := c.extractFallbackData(ctx, trackData); fbData != nil {
		fbSngID := extractVal(fbData["SNG_ID"])
		if fbSngID == "" {
			return "", "", err
		}
		fbU, fbQ, fbErr := c.resolveTrackURL(ctx, fbData, quality, fallback)
		if fbErr == nil {
			trackData["SNG_ID"] = fbSngID
			if fbMD5 := extractVal(fbData["MD5_ORIGIN"]); fbMD5 != "" {
				trackData["MD5_ORIGIN"] = fbMD5
			}
			if fbVer := extractVal(fbData["MEDIA_VERSION"]); fbVer != "" {
				trackData["MEDIA_VERSION"] = fbVer
			}
			return fbU, fbQ, nil
		}
	}

	return "", "", err
}

func (c *Client) extractFallbackData(ctx context.Context, trackData map[string]any) map[string]any {
	if fb, ok := trackData["FALLBACK"].(map[string]any); ok && len(fb) > 0 {
		return fb
	}
	fbID := extractVal(trackData["FALLBACK"])
	sngID := extractVal(trackData["SNG_ID"])
	if fbID != "" && fbID != "0" && fbID != sngID {
		if fbData, err := c.GetTrackData(ctx, fbID); err == nil {
			return fbData
		}
	}
	return nil
}

func (c *Client) resolveTrackURL(ctx context.Context, data map[string]any, quality string, fallback bool) (string, string, error) {
	trackToken, _ := data["TRACK_TOKEN"].(string)

	if trackToken != "" {
		if u, fmtQuality, ok := c.getMediaURL(ctx, trackToken, quality, false); ok {
			return u, fmtQuality, nil
		}
		if fallback {
			for _, fbq := range FallbackQualities(quality) {
				if u, fmtQuality, ok := c.getMediaURL(ctx, trackToken, fbq, false); ok {
					return u, fmtQuality, nil
				}
			}
		}
	}

	md5Origin, _ := data["MD5_ORIGIN"].(string)
	if md5Origin == "" {
		return "", "", errors.New("track unavailable (no MD5_ORIGIN)")
	}

	sngID := extractVal(data["SNG_ID"])
	if sngID == "" {
		return "", "", errors.New("track unavailable (no SNG_ID)")
	}

	mediaVersion := extractVal(data["MEDIA_VERSION"])
	if mediaVersion == "" {
		return "", "", errors.New("track unavailable (no MEDIA_VERSION)")
	}

	qc := QualityCode(quality)
	directURL, err := EncryptDownloadURL(md5Origin, qc, sngID, mediaVersion)
	if err == nil && c.testURL(ctx, directURL) {
		return directURL, quality, nil
	}

	if !fallback {
		return "", "", errors.New("track not available in requested quality")
	}

	for _, fbq := range FallbackQualities(quality) {
		fbCode := QualityCode(fbq)
		fbURL, err := EncryptDownloadURL(md5Origin, fbCode, sngID, mediaVersion)
		if err == nil && c.testURL(ctx, fbURL) {
			return fbURL, fbq, nil
		}
	}

	return "", "", errors.New("no working download URL found for track")
}

func (c *Client) getMediaURL(ctx context.Context, trackToken, quality string, fallback bool) (string, string, bool) {
	licenseToken := c.currentLicense()
	if licenseToken == "" {
		licenseToken = c.refreshLicense(ctx, "")
	}
	if licenseToken == "" {
		return "", "", false
	}

	streamURL, fmtQ, statusCode, errCode, ok := c.doGetMediaURL(ctx, trackToken, licenseToken, quality, fallback)
	if ok {
		return streamURL, fmtQ, true
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || errCode == 2002 {
		if newLic := c.refreshLicense(ctx, licenseToken); newLic != "" && newLic != licenseToken {
			if u2, q2, _, _, ok2 := c.doGetMediaURL(ctx, trackToken, newLic, quality, fallback); ok2 {
				return u2, q2, true
			}
		}
	}

	return "", "", false
}

func (c *Client) doGetMediaURL(ctx context.Context, trackToken, licenseToken, quality string, fallback bool) (streamURL, fmtQuality string, statusCode int, errorCode int, ok bool) {
	formats := []map[string]any{
		{"cipher": "BF_CBC_STRIPE", "format": quality},
	}
	if fallback {
		for _, q := range FallbackQualities(quality) {
			formats = append(formats, map[string]any{
				"cipher": "BF_CBC_STRIPE", "format": q,
			})
		}
	}

	body := map[string]any{
		"license_token": licenseToken,
		"media": []map[string]any{
			{"type": "FULL", "formats": formats},
		},
		"track_tokens": []string{trackToken},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", "", 0, 0, false
	}

	req, err := c.newRequest(ctx, http.MethodPost, mediaAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", 0, 0, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", 0, 0, false
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Errors []struct {
				Code int `json:"code"`
			} `json:"errors"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if len(errResp.Errors) > 0 {
			errorCode = errResp.Errors[0].Code
		}
		return "", "", statusCode, errorCode, false
	}

	var parsed struct {
		Data []struct {
			Media []struct {
				Format  string `json:"format"`
				Sources []struct {
					URL string `json:"url"`
				} `json:"sources"`
			} `json:"media"`
			Errors []struct {
				Code int `json:"code"`
			} `json:"errors"`
		} `json:"data"`
		Errors []struct {
			Code int `json:"code"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", statusCode, 0, false
	}

	if len(parsed.Errors) > 0 {
		errorCode = parsed.Errors[0].Code
		return "", "", statusCode, errorCode, false
	}

	if len(parsed.Data) > 0 && len(parsed.Data[0].Errors) > 0 {
		errorCode = parsed.Data[0].Errors[0].Code
	}

	if len(parsed.Data) == 0 || len(parsed.Data[0].Media) == 0 || len(parsed.Data[0].Media[0].Sources) == 0 {
		return "", "", statusCode, errorCode, false
	}

	media := parsed.Data[0].Media[0]
	streamURL = media.Sources[0].URL
	if !IsAllowedDeezerURL(streamURL) {
		return "", "", statusCode, 0, false
	}
	fmtQuality = media.Format
	if fmtQuality == "" {
		fmtQuality = quality
	}

	return streamURL, fmtQuality, statusCode, errorCode, true
}

func (c *Client) testURL(ctx context.Context, targetURL string) bool {
	req, err := c.newRequest(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-1")

	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false
	}
	if resp.ContentLength == 0 {
		return false
	}

	buf := make([]byte, 1)
	n, _ := io.ReadFull(resp.Body, buf)
	return n > 0
}

type idleResetReader struct {
	r     io.Reader
	timer *time.Timer
	d     time.Duration
}

func (i *idleResetReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.timer.Reset(i.d)
	}
	return n, err
}

func (c *Client) DownloadTrack(ctx context.Context, trackID, dstPath string, maxBytes int64) (actualQuality, ext string, track *Track, err error) {
	trackData, err := c.GetTrackData(ctx, trackID)
	if err != nil {
		return "", "", nil, fmt.Errorf("track metadata: %w", err)
	}

	track = parseTrackFromGW(trackData)

	preferred := c.PreferredQuality()
	downloadURL, actualQuality, err := c.GetTrackDownloadURL(ctx, trackData, preferred, true)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve download url: %w", err)
	}

	ext = QualityExt(actualQuality)
	sngID := extractVal(trackData["SNG_ID"])
	bfKey := GetBlowfishKey(sngID)

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := time.AfterFunc(downloadIdleTimeout, cancel)
	defer idle.Stop()
	stalled := func(err error) error {
		if ctx.Err() == nil && dlCtx.Err() != nil {
			return fmt.Errorf("no data received for %s: %w", downloadIdleTimeout, err)
		}
		return err
	}

	req, err := c.newRequest(dlCtx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", "", nil, err
	}

	dlClient := *c.http
	dlClient.Timeout = 0
	resp, err := dlClient.Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("download: %w", stalled(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", "", nil, fmt.Errorf("download server returned HTTP %d", resp.StatusCode)
	}

	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return "", "", nil, ErrFileTooLarge
	}

	file, err := os.Create(dstPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	idle.Reset(downloadIdleTimeout)
	n, err := DecryptDeezerStream(&idleResetReader{r: resp.Body, timer: idle, d: downloadIdleTimeout}, file, bfKey, maxBytes)
	if err == nil && resp.ContentLength > 0 && n != resp.ContentLength {
		err = fmt.Errorf("received %d of %d bytes", n, resp.ContentLength)
	}
	if err != nil {
		_ = file.Close()
		_ = os.Remove(dstPath)
		return "", "", nil, fmt.Errorf("decrypt stream: %w", stalled(err))
	}
	if n == 0 {
		_ = file.Close()
		_ = os.Remove(dstPath)
		return "", "", nil, errors.New("download failed: empty response")
	}

	return actualQuality, ext, track, nil
}

func parseTrackFromGW(data map[string]any) *Track {
	id, _ := strconv.ParseInt(extractVal(data["SNG_ID"]), 10, 64)
	albumID, _ := strconv.ParseInt(extractVal(data["ALB_ID"]), 10, 64)
	artistID, _ := strconv.ParseInt(extractVal(data["ART_ID"]), 10, 64)

	title := extractVal(data["SNG_TITLE"])
	if version := extractVal(data["VERSION"]); version != "" {
		title = fmt.Sprintf("%s (%s)", title, version)
	}

	duration, _ := strconv.ParseFloat(extractVal(data["DURATION"]), 64)
	trackPos, _ := strconv.Atoi(extractVal(data["TRACK_NUMBER"]))
	discNum, _ := strconv.Atoi(extractVal(data["DISK_NUMBER"]))
	releaseDate := extractVal(data["PHYSICAL_RELEASE_DATE"])
	if releaseDate == "" {
		releaseDate = extractVal(data["DIGITAL_RELEASE_DATE"])
	}

	albPicture := extractVal(data["ALB_PICTURE"])
	var coverSmall, coverMedium, coverBig, coverXL string
	if albPicture != "" {
		coverSmall = fmt.Sprintf("https://e-cdns-images.dzcdn.net/images/cover/%s/56x56-000000-80-0-0.jpg", albPicture)
		coverMedium = fmt.Sprintf("https://e-cdns-images.dzcdn.net/images/cover/%s/250x250-000000-80-0-0.jpg", albPicture)
		coverBig = fmt.Sprintf("https://e-cdns-images.dzcdn.net/images/cover/%s/500x500-000000-80-0-0.jpg", albPicture)
		coverXL = fmt.Sprintf("https://e-cdns-images.dzcdn.net/images/cover/%s/1000x1000-000000-80-0-0.jpg", albPicture)
	}

	return &Track{
		ID:            id,
		Title:         strings.TrimSpace(title),
		Artist:        strings.TrimSpace(extractVal(data["ART_NAME"])),
		ArtistID:      artistID,
		Album:         strings.TrimSpace(extractVal(data["ALB_TITLE"])),
		AlbumID:       albumID,
		Duration:      duration,
		TrackPosition: trackPos,
		DiskNumber:    discNum,
		ReleaseDate:   releaseDate,
		Genre:         strings.TrimSpace(extractVal(data["GENRE"])),
		CoverSmall:    coverSmall,
		CoverMedium:   coverMedium,
		CoverBig:      coverBig,
		CoverXL:       coverXL,
		Link:          fmt.Sprintf("https://www.deezer.com/track/%d", id),
	}
}

func extractVal(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case json.Number:
		return v.String()
	case map[string]any, []any:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
