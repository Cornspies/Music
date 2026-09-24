package server

import (
	"context"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ncsSource struct{}

const (
	ncsBaseURL   = "https://ncs.io"
	ncsUserAgent = "Mozilla/5.0 (compatible; Music-SelfHosted-MusicPlayer/1.0)"
	ncsPageSize  = 20
)

func (ncsSource) coverAllowed(u string) bool {
	return strings.HasPrefix(u, "https://ncsmusic.s3.eu-west-1.amazonaws.com/")
}

func (ncsSource) key() string  { return "ncs" }
func (ncsSource) name() string { return "NCS" }

var (
	ncsRowRe        = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	ncsPlayRe       = regexp.MustCompile(`(?s)<a\s+class="[^"]*player-play[^"]*".*?><i\s`)
	ncsAttrRe       = regexp.MustCompile(`data-([a-z]+)="([^"]*)"`)
	ncsDateRe       = regexp.MustCompile(`<td[^>]*>\s*(\d{1,2}\s+[A-Za-z]{3}\s+(\d{4}))\s*</td>`)
	ncsPageLinkRe   = regexp.MustCompile(`<td[^>]*><a href="(/[^"]+)">`)
	ncsNextRe       = regexp.MustCompile(`page=(\d+)`)
	ncsCoverSize    = regexp.MustCompile(`/\d+x\d+/`)
	ncsPanelTitleRe = regexp.MustCompile(`(?s)<h5>(.*?)<span>(.*?)</span>`)
	ncsPanelLinkRe  = regexp.MustCompile(`<a href="/track/download/[^"]*"[^>]*>`)
	ncsPanelCoverRe = regexp.MustCompile(`background-image:\s*url\('([^']+)'\)`)
)

func ncsGet(ctx context.Context, client *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ncsUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ncs: %s: status %d", u, resp.StatusCode)
	}
	return resp, nil
}

func (n ncsSource) search(ctx context.Context, query string, page int) ([]discoverResult, bool, error) {
	q := url.Values{}
	q.Set("q", query)
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	resp, err := ncsGet(ctx, metadataLookupClient, ncsBaseURL+"/music-search?"+q.Encode())
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, false, err
	}
	results := parseNCSSearch(string(body))
	hasMore := false
	for _, m := range ncsNextRe.FindAllStringSubmatch(string(body), -1) {
		if p, _ := strconv.Atoi(m[1]); p == page+1 {
			hasMore = true
			break
		}
	}
	return results, hasMore && len(results) == ncsPageSize, nil
}

func parseNCSSearch(page string) []discoverResult {
	var out []discoverResult
	for _, row := range ncsRowRe.FindAllStringSubmatch(page, -1) {
		play := ncsPlayRe.FindString(row[1])
		if play == "" {
			continue
		}
		attrs := map[string]string{}
		for _, m := range ncsAttrRe.FindAllStringSubmatch(play, -1) {
			attrs[m[1]] = html.UnescapeString(m[2])
		}
		id := attrs["tid"]
		if id == "" || id == "0" || attrs["track"] == "" {
			continue
		}
		r := discoverResult{
			Source:     "ncs",
			ID:         id,
			Title:      strings.TrimSpace(attrs["track"]),
			Artist:     strings.TrimSpace(attrs["artistraw"]),
			Genre:      strings.TrimSpace(attrs["genre"]),
			CoverURL:   ncsCoverSize.ReplaceAllString(attrs["cover"], "/325x325/"),
			PreviewURL: attrs["url"],
		}
		if m := ncsDateRe.FindStringSubmatch(row[1]); m != nil {
			r.Year, _ = strconv.Atoi(m[2])
		}
		if m := ncsPageLinkRe.FindStringSubmatch(row[1]); m != nil {
			r.PageURL = ncsBaseURL + m[1]
		}
		out = append(out, r)
	}
	return out
}

func ncsFullCover(thumb string) string {
	if thumb == "" {
		return ""
	}
	return ncsCoverSize.ReplaceAllString(thumb, "/")
}

func (n ncsSource) fetch(ctx context.Context, id string) (*sourceDownload, error) {
	if strings.ContainsAny(id, "/?#& ") {
		return nil, fmt.Errorf("ncs: bad id %q", id)
	}
	title, artist, genre, cover := n.panelInfo(ctx, id)
	year := 0
	if title != "" {
		if hits, _, err := n.search(ctx, title, 1); err == nil {
			for _, h := range hits {
				if h.ID == id {
					year, genre = h.Year, trimDefault(h.Genre, genre)
					if h.CoverURL != "" {
						cover = ncsFullCover(h.CoverURL)
					}
					break
				}
			}
		}
	}

	resp, err := ncsGet(ctx, discoverClient, ncsBaseURL+"/track/download/"+id)
	if err != nil {
		return nil, err
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "audio/") {
		resp.Body.Close()
		return nil, fmt.Errorf("ncs: download returned %q, not audio (track gone, or the site changed)", ct)
	}
	fileName := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		fileName = path.Base(params["filename"])
	}
	if fileName == "" || fileName == "." || path.Ext(fileName) == "" {
		fileName = trimDefault(artist, "NCS") + " - " + trimDefault(title, id) + ".mp3"
	}
	return &sourceDownload{
		Body:     resp.Body,
		FileName: fileName,
		Size:     resp.ContentLength,
		Title:    title,
		Artist:   artist,
		Album:    title,
		Genre:    genre,
		Year:     year,
		CoverURL: cover,
	}, nil
}

func (ncsSource) panelInfo(ctx context.Context, id string) (title, artist, genre, cover string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := ncsGet(ctx, metadataLookupClient, ncsBaseURL+"/track/info/"+id)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return
	}
	s := string(body)
	if m := ncsPanelTitleRe.FindStringSubmatch(s); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
		artist = strings.TrimSpace(html.UnescapeString(m[2]))
	}
	if link := ncsPanelLinkRe.FindString(s); link != "" {
		for _, m := range ncsAttrRe.FindAllStringSubmatch(link, -1) {
			switch m[1] {
			case "track":
				title = trimDefault(title, html.UnescapeString(m[2]))
			case "artist":
				artist = trimDefault(artist, html.UnescapeString(m[2]))
			case "genre":
				genre = html.UnescapeString(m[2])
			}
		}
	}
	if m := ncsPanelCoverRe.FindStringSubmatch(s); m != nil {
		cover = ncsFullCover(m[1])
	}
	return
}
