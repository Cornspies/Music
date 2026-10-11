package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ytMusicSearchURL     = "https://music.youtube.com/youtubei/v1/search?prettyPrint=false"
	ytMusicClientVersion = "1.20250101.01.00"
	ytMusicSongsFilter   = "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D"
)

var ytMusicClient = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: metadataLookupClient.CheckRedirect,
	Transport:     metadataLookupClient.Transport,
}

type ytmRun struct {
	Text               string `json:"text"`
	NavigationEndpoint *struct {
		WatchEndpoint *struct {
			VideoID string `json:"videoId"`
		} `json:"watchEndpoint"`
		BrowseEndpoint *struct {
			BrowseEndpointContextSupportedConfigs struct {
				BrowseEndpointContextMusicConfig struct {
					PageType string `json:"pageType"`
				} `json:"browseEndpointContextMusicConfig"`
			} `json:"browseEndpointContextSupportedConfigs"`
		} `json:"browseEndpoint"`
	} `json:"navigationEndpoint"`
}

type ytmItem struct {
	PlaylistItemData *struct {
		VideoID string `json:"videoId"`
	} `json:"playlistItemData"`
	FlexColumns []struct {
		Renderer struct {
			Text struct {
				Runs []ytmRun `json:"runs"`
			} `json:"text"`
		} `json:"musicResponsiveListItemFlexColumnRenderer"`
	} `json:"flexColumns"`
}

func (y *ytdlpSource) searchSongs(ctx context.Context, query string) ([]discoverResult, error) {
	body, _ := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]string{
			"clientName": "WEB_REMIX", "clientVersion": ytMusicClientVersion, "hl": "en", "gl": "US",
		}},
		"query":  query,
		"params": ytMusicSongsFilter,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ytMusicSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")
	resp, err := ytMusicClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtube music search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music search: status %d", resp.StatusCode)
	}
	var parsed any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("youtube music search: %w", err)
	}
	var raw []json.RawMessage
	collectYTMItems(parsed, &raw)
	results := []discoverResult{}
	for _, msg := range raw {
		var it ytmItem
		if json.Unmarshal(msg, &it) != nil {
			continue
		}
		if r, ok := y.songResult(it); ok {
			results = append(results, r)
		}
	}
	return results, nil
}

func collectYTMItems(v any, out *[]json.RawMessage) {
	switch n := v.(type) {
	case map[string]any:
		if item, ok := n["musicResponsiveListItemRenderer"]; ok {
			if b, err := json.Marshal(item); err == nil {
				*out = append(*out, b)
			}
			return
		}
		for _, child := range n {
			collectYTMItems(child, out)
		}
	case []any:
		for _, child := range n {
			collectYTMItems(child, out)
		}
	}
}

func (y *ytdlpSource) songResult(it ytmItem) (discoverResult, bool) {
	if len(it.FlexColumns) < 2 {
		return discoverResult{}, false
	}
	id := ""
	if it.PlaylistItemData != nil {
		id = it.PlaylistItemData.VideoID
	}
	var title strings.Builder
	for _, run := range it.FlexColumns[0].Renderer.Text.Runs {
		title.WriteString(run.Text)
		if id == "" && run.NavigationEndpoint != nil && run.NavigationEndpoint.WatchEndpoint != nil {
			id = run.NavigationEndpoint.WatchEndpoint.VideoID
		}
	}
	if !youtubeIDRe.MatchString(id) || strings.TrimSpace(title.String()) == "" {
		return discoverResult{}, false
	}

	var artists, album []string
	duration := 0.0
	segment := 0
	for _, run := range it.FlexColumns[1].Renderer.Text.Runs {
		text := strings.TrimSpace(run.Text)
		if text == "•" {
			segment++
			continue
		}
		pageType := ""
		if run.NavigationEndpoint != nil && run.NavigationEndpoint.BrowseEndpoint != nil {
			pageType = run.NavigationEndpoint.BrowseEndpoint.BrowseEndpointContextSupportedConfigs.BrowseEndpointContextMusicConfig.PageType
		}
		switch {
		case pageType == "MUSIC_PAGE_TYPE_ARTIST" || pageType == "MUSIC_PAGE_TYPE_USER_CHANNEL":
			artists = append(artists, text)
		case pageType == "MUSIC_PAGE_TYPE_ALBUM":
			album = append(album, text)
		case parseClock(text) > 0:
			duration = parseClock(text)
		case segment == 0 && text != "" && text != "&" && text != "," && !strings.EqualFold(text, "Song"):
			artists = append(artists, text)
		}
	}
	return discoverResult{
		Source:   y.key(),
		ID:       id,
		Title:    strings.TrimSpace(title.String()),
		Artist:   strings.Join(artists, ", "),
		Album:    strings.Join(album, " "),
		Duration: duration,
		PageURL:  "https://music.youtube.com/watch?v=" + id,
		CoverURL: "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg",
	}, true
}

func parseClock(s string) float64 {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return float64(total)
}
