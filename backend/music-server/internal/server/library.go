package server

import (
	"net/http"
	"slices"
	"strings"
)

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := s.db.Query(trackSelect+`
                JOIN favorites fav ON fav.track_id = t.id AND fav.user_id = ?
                ORDER BY fav.created_at DESC`, append(trackSelectArgs(u.ID), u.ID)...)
	if err != nil {
		s.internalErr(w, "list favorites", err)
		return
	}
	defer rows.Close()

	tracks := []*Track{}
	for rows.Next() {
		t, _, _, err := scanTrack(rows.Scan)
		if err != nil {
			s.internalErr(w, "scan favorite", err)
			return
		}
		tracks = append(tracks, t)
	}
	writeJSON(w, http.StatusOK, tracks)
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	trackID := r.PathValue("trackId")

	if !s.canSeeTrack(w, u.ID, trackID) {
		return
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO favorites (user_id, track_id, created_at) VALUES (?,?,?)`,
		u.ID, trackID, nowRFC3339())
	if err != nil {
		s.internalErr(w, "add favorite", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.noteFavoriteChange(u.ID, trackID)
	}
	writeNoContent(w)
}

func (s *Server) noteFavoriteChange(userID, trackID string) {
	if _, err := s.db.Exec(`INSERT INTO favorite_changes (user_id, track_id, changed_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id, track_id) DO UPDATE SET changed_at = excluded.changed_at`, userID, trackID, nowRFC3339()); err != nil {
		logErr("note favorite change", err)
	}
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	trackID := r.PathValue("trackId")
	res, err := s.db.Exec(`DELETE FROM favorites WHERE user_id = ? AND track_id = ?`, u.ID, trackID)
	if err != nil {
		s.internalErr(w, "remove favorite", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.noteFavoriteChange(u.ID, trackID)
	}
	writeNoContent(w)
}

type Album struct {
	Name          string  `json:"name"`
	Artist        string  `json:"artist"`
	TrackCount    int     `json:"trackCount"`
	TotalDuration float64 `json:"totalDuration"`
	HasCover      bool    `json:"hasCover"`
	CoverTrackID  string  `json:"coverTrackId"`
}

type Artist struct {
	Name          string  `json:"name"`
	TrackCount    int     `json:"trackCount"`
	TotalDuration float64 `json:"totalDuration"`
}

func (s *Server) handleListAlbums(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := s.db.Query(`
                SELECT
                        COALESCE(NULLIF(t.album, ''), 'Unknown Album') AS album_name,
                        COALESCE(NULLIF(t.artist, ''), 'Unknown Artist') AS artist_name,
                        COUNT(*),
                        COALESCE(SUM(t.duration), 0),
                        MAX(t.has_cover),
                        COALESCE((SELECT t2.id FROM tracks t2
                                  WHERE t2.album = t.album AND t2.artist = t.artist AND t2.has_cover = 1
                                    AND `+visibleTo("t2.user_id")+`
                                  ORDER BY t2.created_at LIMIT 1), '')
                FROM tracks t
                WHERE `+visibleTo("t.user_id")+`
                GROUP BY album_name, artist_name
                ORDER BY album_name COLLATE NOCASE ASC`, u.ID, u.ID)
	if err != nil {
		s.internalErr(w, "list albums", err)
		return
	}
	defer rows.Close()

	albums := []*Album{}
	for rows.Next() {
		var a Album
		var hasCover int
		if err := rows.Scan(&a.Name, &a.Artist, &a.TrackCount, &a.TotalDuration, &hasCover, &a.CoverTrackID); err != nil {
			s.internalErr(w, "scan album", err)
			return
		}
		a.HasCover = hasCover == 1
		albums = append(albums, &a)
	}
	writeJSON(w, http.StatusOK, albums)
}

func (s *Server) handleListArtists(w http.ResponseWriter, r *http.Request) {
	artists, err := s.libraryArtists(userFrom(r).ID, "")
	if err != nil {
		s.internalErr(w, "list artists", err)
		return
	}
	writeJSON(w, http.StatusOK, artists)
}

func (s *Server) libraryArtists(userID, filter string) ([]*Artist, error) {
	rows, err := s.db.Query(`SELECT t.artist, t.duration FROM tracks t WHERE `+visibleTo("t.user_id"), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type tally struct {
		artist   Artist
		spelling map[string]int
	}
	byKey := map[string]*tally{}
	var order []string
	filter = artistKey(filter)
	for rows.Next() {
		var credit string
		var duration float64
		if err := rows.Scan(&credit, &duration); err != nil {
			return nil, err
		}
		names := splitArtists(dropPlaceholder(credit, "Unknown Artist"))
		if len(names) == 0 {
			names = []string{"Unknown Artist"}
		}
		for _, name := range names {
			key := artistKey(name)
			if filter != "" && !strings.Contains(key, filter) {
				continue
			}
			t := byKey[key]
			if t == nil {
				t = &tally{spelling: map[string]int{}}
				byKey[key] = t
				order = append(order, key)
			}
			t.artist.TrackCount++
			t.artist.TotalDuration += duration
			t.spelling[name]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Artist, 0, len(order))
	for _, key := range order {
		t := byKey[key]
		best := 0
		for name, n := range t.spelling {
			if n > best || n == best && name < t.artist.Name {
				t.artist.Name, best = name, n
			}
		}
		a := t.artist
		out = append(out, &a)
	}
	slices.SortFunc(out, func(a, b *Artist) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	result := map[string]any{"tracks": []*Track{}, "albums": []*Album{}, "artists": []*Artist{}, "playlists": []*Playlist{}}
	if q == "" {
		writeJSON(w, http.StatusOK, result)
		return
	}
	like := "%" + likeEscape(q) + "%"
	match := `(t.title LIKE ? ESCAPE '\' OR t.artist LIKE ? ESCAPE '\' OR t.album LIKE ? ESCAPE '\' OR t.genre LIKE ? ESCAPE '\')`
	limit := " LIMIT 50"
	if r.URL.Query().Get("tracks") == "all" {
		limit = ""
	}

	rows, err := s.db.Query(trackSelect+`
                WHERE `+match+`
                ORDER BY COALESCE(ps.plays, 0) DESC, t.created_at DESC`+limit,
		append(trackSelectArgs(u.ID), like, like, like, like)...)
	if err != nil {
		s.internalErr(w, "search tracks", err)
		return
	}
	defer rows.Close()
	tracks := []*Track{}
	for rows.Next() {
		t, _, _, err := scanTrack(rows.Scan)
		if err != nil {
			s.internalErr(w, "scan search track", err)
			return
		}
		tracks = append(tracks, t)
	}
	result["tracks"] = tracks
	tracksTotal := len(tracks)
	if limit != "" && len(tracks) == 50 {
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM tracks t WHERE `+match+` AND `+visibleTo("t.user_id"),
			like, like, like, like, u.ID).Scan(&tracksTotal)
	}
	result["tracksTotal"] = tracksTotal

	rows2, err := s.db.Query(`
                SELECT COALESCE(NULLIF(t.album,''),'Unknown Album'), COALESCE(NULLIF(t.artist,''),'Unknown Artist'), COUNT(*), COALESCE(SUM(t.duration),0), MAX(t.has_cover),
                       COALESCE((SELECT t2.id FROM tracks t2 WHERE t2.album = t.album AND t2.artist = t.artist AND t2.has_cover = 1
                                   AND `+visibleTo("t2.user_id")+` ORDER BY t2.created_at LIMIT 1), '')
                FROM tracks t
                WHERE (t.album LIKE ? ESCAPE '\' OR t.artist LIKE ? ESCAPE '\') AND `+visibleTo("t.user_id")+`
                GROUP BY 1, 2 ORDER BY 1 COLLATE NOCASE ASC LIMIT 25`, u.ID, like, like, u.ID)
	if err != nil {
		s.internalErr(w, "search albums", err)
		return
	}
	defer rows2.Close()
	albums := []*Album{}
	for rows2.Next() {
		var a Album
		var hasCover int
		if err := rows2.Scan(&a.Name, &a.Artist, &a.TrackCount, &a.TotalDuration, &hasCover, &a.CoverTrackID); err != nil {
			s.internalErr(w, "scan search album", err)
			return
		}
		a.HasCover = hasCover == 1
		albums = append(albums, &a)
	}
	result["albums"] = albums

	artists, err := s.libraryArtists(u.ID, q)
	if err != nil {
		s.internalErr(w, "search artists", err)
		return
	}
	if len(artists) > 25 {
		artists = artists[:25]
	}
	result["artists"] = artists

	rows4, err := s.db.Query(playlistSelect+`
                WHERE (p.user_id = ? OR p.is_public = 1) AND `+visibleTo("p.user_id")+` AND p.name LIKE ? ESCAPE '\'
                ORDER BY p.created_at DESC LIMIT 25`, append(playlistSelectArgs(u.ID), u.ID, u.ID, like)...)
	if err != nil {
		s.internalErr(w, "search playlists", err)
		return
	}
	defer rows4.Close()
	playlists := []*Playlist{}
	for rows4.Next() {
		p, err := scanPlaylist(rows4.Scan, u.ID)
		if err != nil {
			s.internalErr(w, "scan search playlist", err)
			return
		}
		playlists = append(playlists, p)
	}
	result["playlists"] = playlists

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)

	stats := map[string]any{}
	var trackCount, artistCount, albumCount, favoriteCount, playlistCount int
	var totalDuration float64

	if err := s.db.QueryRow(`
                SELECT COUNT(*), COALESCE(SUM(t.duration),0), COUNT(DISTINCT COALESCE(NULLIF(t.album,''),'Unknown Album'))
                FROM tracks t WHERE `+visibleTo("t.user_id"), u.ID).
		Scan(&trackCount, &totalDuration, &albumCount); err != nil {
		s.internalErr(w, "stats tracks", err)
		return
	}
	if artists, err := s.libraryArtists(u.ID, ""); err == nil {
		artistCount = len(artists)
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM favorites WHERE user_id = ?`, u.ID).Scan(&favoriteCount); err != nil {
		s.internalErr(w, "stats favorites", err)
		return
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM playlists WHERE user_id = ?`, u.ID).Scan(&playlistCount); err != nil {
		s.internalErr(w, "stats playlists", err)
		return
	}

	var listenedSeconds float64
	var listenCount int
	if err := s.db.QueryRow(`
                SELECT COUNT(1), COALESCE(SUM(`+listenedExpr+`), 0)
                FROM play_events pe JOIN tracks t ON t.id = pe.track_id
                WHERE pe.user_id = ? AND `+visibleTo("t.user_id"), u.ID, u.ID).Scan(&listenCount, &listenedSeconds); err != nil {
		s.internalErr(w, "stats listening", err)
		return
	}
	stats["listenedSeconds"] = listenedSeconds
	stats["playCount"] = listenCount

	stats["trackCount"] = trackCount
	stats["totalDuration"] = totalDuration
	stats["artistCount"] = artistCount
	stats["albumCount"] = albumCount
	stats["favoriteCount"] = favoriteCount
	stats["playlistCount"] = playlistCount

	rows, err := s.db.Query(trackSelect+`
                WHERE COALESCE(ps.plays, 0) > 0
                ORDER BY ps.plays DESC, t.created_at DESC LIMIT 10`, trackSelectArgs(u.ID)...)
	if err != nil {
		s.internalErr(w, "stats most played", err)
		return
	}
	defer rows.Close()
	mostPlayed := []*Track{}
	for rows.Next() {
		t, _, _, err := scanTrack(rows.Scan)
		if err != nil {
			s.internalErr(w, "scan most played", err)
			return
		}
		mostPlayed = append(mostPlayed, t)
	}
	stats["mostPlayed"] = mostPlayed

	recent, err := s.db.Query(strings.Replace(trackSelect, "\nFROM tracks t", ",\n       pe.played_at\nFROM tracks t", 1)+`
                JOIN (SELECT track_id, MAX(played_at) AS played_at FROM play_events
                      WHERE user_id = ? GROUP BY track_id) pe ON pe.track_id = t.id
                ORDER BY pe.played_at DESC LIMIT 10`, append(trackSelectArgs(u.ID), u.ID)...)
	if err != nil {
		s.internalErr(w, "stats recently played", err)
		return
	}
	defer recent.Close()
	recentlyPlayed := []*Track{}
	for recent.Next() {
		var playedAt string
		t, _, _, err := scanTrackWith(recent.Scan, &playedAt)
		if err != nil {
			s.internalErr(w, "scan recently played", err)
			return
		}
		recentlyPlayed = append(recentlyPlayed, t)
	}
	stats["recentlyPlayed"] = recentlyPlayed

	writeJSON(w, http.StatusOK, stats)
}
