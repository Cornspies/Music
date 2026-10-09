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
	maxTrackBytes = 1024 * 1024 * 1024 // 1 GiB safety cap
)

// Client interacts with Deezer via private gw-light and official public APIs.
type Client struct {
	http             *http.Client
	arl              string
	token            string
	licenseToken     string
	user             *UserInfo
	preferredQuality string
	mu               sync.RWMutex
}

// NewClient initializes a Deezer client with optional ARL and preferred audio quality.
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

	q := strings.ToUpper(strings.TrimSpace(preferredQuality))
	if q == "" {
		if trimmedARL != "" {
			q = QualityMP3_320
		} else {
			q = QualityMP3_128
		}
	}

	c := &Client{
		http: &http.Client{
			Jar:     jar,
			Timeout: 45 * time.Second,
		},
		arl:              trimmedARL,
		preferredQuality: q,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := c.login(ctx); err != nil {
		// If ARL was explicitly provided and failed, log or report error.
		// If no ARL was provided, login failure to gw-light should not block public API usage,
		// but we still try our best.
		if trimmedARL != "" {
			return nil, fmt.Errorf("deezer login failed: %w", err)
		}
	}

	return c, nil
}

// User returns the logged in Deezer user, or nil if anonymous.
func (c *Client) User() *UserInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

// PreferredQuality returns the preferred audio quality preset.
func (c *Client) PreferredQuality() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.preferredQuality
}

// HasARL returns whether the client was configured with an ARL token.
func (c *Client) HasARL() bool {
	return c.arl != ""
}

func (c *Client) newRequest(ctx context.Context, method, targetURL string, body io.Reader) (*http.Request, error) {
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

// login establishes a Deezer web session and obtains CSRF token (checkForm) and user info.
func (c *Client) login(ctx context.Context) error {
	// 1. Initial request to establish session and get SID cookie
	initReq, err := c.newRequest(ctx, http.MethodGet, gwURL, nil)
	if err != nil {
		return err
	}
	initResp, err := c.http.Do(initReq)
	if err != nil {
		return fmt.Errorf("session init: %w", err)
	}
	_ = initResp.Body.Close()

	// 2. Fetch user data with api_token = "null"
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

	if uid > 0 {
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

// rawAPICall makes an HTTP POST to gw-light.php with api_token and method.
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

// APICall executes a method on Deezer's gw-light API, automatically re-authenticating if needed.
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
		// Try refreshing session once
		if rerr := c.login(ctx); rerr == nil {
			c.mu.RLock()
			token = c.token
			c.mu.RUnlock()
			return c.rawAPICall(ctx, method, token, params)
		}
	}
	return data, err
}

// GetTrackData retrieves private track info (including MD5_ORIGIN and MEDIA_VERSION) via song.getData.
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

	// Some responses wrap song data inside "DATA"
	if inner, ok := resp.Results["DATA"].(map[string]any); ok && len(inner) > 0 {
		return inner, nil
	}
	return resp.Results, nil
}

// GetTrackDownloadURL resolves the CDN or media streaming URL and actual quality for a track.
func (c *Client) GetTrackDownloadURL(ctx context.Context, trackData map[string]any, quality string, fallback bool) (string, string, error) {
	c.mu.RLock()
	licenseToken := c.licenseToken
	c.mu.RUnlock()

	trackToken, _ := trackData["TRACK_TOKEN"].(string)

	// 1. Try media.deezer.com/v1/get_url if track_token and license_token are available
	if trackToken != "" && licenseToken != "" {
		if u, fmtQuality, ok := c.getMediaURL(ctx, trackToken, licenseToken, quality, false); ok {
			return u, fmtQuality, nil
		}
		if fallback {
			for _, fbq := range FallbackQualities(quality) {
				if u, fmtQuality, ok := c.getMediaURL(ctx, trackToken, licenseToken, fbq, false); ok {
					return u, fmtQuality, nil
				}
			}
		}
	}

	// 2. Fallback to encrypted mobile CDN URL
	md5Origin, _ := trackData["MD5_ORIGIN"].(string)
	if md5Origin == "" {
		return "", "", errors.New("track unavailable (no MD5_ORIGIN)")
	}

	sngID := extractVal(trackData["SNG_ID"])
	if sngID == "" {
		return "", "", errors.New("track unavailable (no SNG_ID)")
	}

	mediaVersion := extractVal(trackData["MEDIA_VERSION"])
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

// getMediaURL calls media.deezer.com/v1/get_url to get an authenticated stream URL.
func (c *Client) getMediaURL(ctx context.Context, trackToken, licenseToken, quality string, fallback bool) (string, string, bool) {
	formats := []map[string]any{
		{"cipher": "BF_CBC_STRIPE", "format": quality},
	}
	if fallback {
		for _, q := range []string{QualityMP3_320, QualityMP3_128, QualityFLAC} {
			if q != quality {
				formats = append(formats, map[string]any{
					"cipher": "BF_CBC_STRIPE", "format": q,
				})
			}
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
		return "", "", false
	}

	req, err := c.newRequest(ctx, http.MethodPost, mediaAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", false
	}

	var parsed struct {
		Data []struct {
			Media []struct {
				Format  string `json:"format"`
				Sources []struct {
					URL string `json:"url"`
				} `json:"sources"`
			} `json:"media"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", false
	}

	if len(parsed.Data) == 0 || len(parsed.Data[0].Media) == 0 || len(parsed.Data[0].Media[0].Sources) == 0 {
		return "", "", false
	}

	media := parsed.Data[0].Media[0]
	streamURL := media.Sources[0].URL
	fmtQuality := media.Format
	if fmtQuality == "" {
		fmtQuality = quality
	}

	return streamURL, fmtQuality, true
}

func (c *Client) testURL(ctx context.Context, targetURL string) bool {
	req, err := c.newRequest(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return false
	}
	// Use Range header to only fetch the first few bytes for verification
	req.Header.Set("Range", "bytes=0-1")

	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// 200 OK or 206 Partial Content indicates stream is valid and reachable
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// DownloadTrack downloads and decrypts the track into dstPath, enforcing maxBytes limit if > 0.
func (c *Client) DownloadTrack(ctx context.Context, trackID, dstPath string, maxBytes int64) (actualQuality, ext string, track *Track, err error) {
	trackData, err := c.GetTrackData(ctx, trackID)
	if err != nil {
		return "", "", nil, fmt.Errorf("track metadata: %w", err)
	}

	preferred := c.PreferredQuality()
	downloadURL, actualQuality, err := c.GetTrackDownloadURL(ctx, trackData, preferred, true)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve download url: %w", err)
	}

	ext = QualityExt(actualQuality)
	sngID := extractVal(trackData["SNG_ID"])
	bfKey := GetBlowfishKey(sngID)

	req, err := c.newRequest(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", "", nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("download: %w", err)
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

	n, err := DecryptDeezerStream(resp.Body, file, bfKey, maxBytes)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(dstPath)
		return "", "", nil, fmt.Errorf("decrypt stream: %w", err)
	}
	if n == 0 {
		_ = file.Close()
		_ = os.Remove(dstPath)
		return "", "", nil, errors.New("download failed: empty response")
	}

	track = parseTrackFromGW(trackData)
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
	default:
		return fmt.Sprintf("%v", v)
	}
}
