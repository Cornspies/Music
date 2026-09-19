package server

import (
        "net/http"
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

        var exists int
        if err := s.db.QueryRow(`SELECT COUNT(1) FROM tracks WHERE id = ?`, trackID).Scan(&exists); err != nil {
                s.internalErr(w, "check track", err)
                return
        }
        if exists == 0 {
                writeErr(w, http.StatusNotFound, "track not found")
                return
        }
        if _, err := s.db.Exec(`INSERT OR IGNORE INTO favorites (user_id, track_id, created_at) VALUES (?,?,?)`,
                u.ID, trackID, nowRFC3339()); err != nil {
                s.internalErr(w, "add favorite", err)
                return
        }
        writeNoContent(w)
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        trackID := r.PathValue("trackId")
        if _, err := s.db.Exec(`DELETE FROM favorites WHERE user_id = ? AND track_id = ?`, u.ID, trackID); err != nil {
                s.internalErr(w, "remove favorite", err)
                return
        }
        writeNoContent(w)
}

// ---- aggregates / search / stats ----

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
        // Albums and artists are aggregated over the whole shared library,
        // so there's no per-user filter here any more.
        rows, err := s.db.Query(`
                SELECT
                        COALESCE(NULLIF(t.album, ''), 'Unknown Album') AS album_name,
                        COALESCE(NULLIF(t.artist, ''), 'Unknown Artist') AS artist_name,
                        COUNT(*),
                        COALESCE(SUM(t.duration), 0),
                        MAX(t.has_cover),
                        COALESCE((SELECT t2.id FROM tracks t2
                                  WHERE t2.album = t.album AND t2.artist = t.artist AND t2.has_cover = 1
                                  ORDER BY t2.created_at LIMIT 1), '')
                FROM tracks t
                GROUP BY album_name, artist_name
                ORDER BY album_name COLLATE NOCASE ASC`)
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
        // Albums and artists are aggregated over the whole shared library,
        // so there's no per-user filter here any more.
        rows, err := s.db.Query(`
                SELECT COALESCE(NULLIF(artist, ''), 'Unknown Artist'), COUNT(*), COALESCE(SUM(duration), 0)
                FROM tracks
                GROUP BY 1 ORDER BY 1 COLLATE NOCASE ASC`)
        if err != nil {
                s.internalErr(w, "list artists", err)
                return
        }
        defer rows.Close()

        artists := []*Artist{}
        for rows.Next() {
                var a Artist
                if err := rows.Scan(&a.Name, &a.TrackCount, &a.TotalDuration); err != nil {
                        s.internalErr(w, "scan artist", err)
                        return
                }
                artists = append(artists, &a)
        }
        writeJSON(w, http.StatusOK, artists)
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

        rows, err := s.db.Query(trackSelect+`
                WHERE (t.title LIKE ? ESCAPE '\' OR t.artist LIKE ? ESCAPE '\' OR t.album LIKE ? ESCAPE '\' OR t.genre LIKE ? ESCAPE '\')
                ORDER BY COALESCE(ps.plays, 0) DESC, t.created_at DESC LIMIT 50`,
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

        rows2, err := s.db.Query(`
                SELECT COALESCE(NULLIF(t.album,''),'Unknown Album'), COALESCE(NULLIF(t.artist,''),'Unknown Artist'), COUNT(*), COALESCE(SUM(t.duration),0), MAX(t.has_cover),
                       COALESCE((SELECT t2.id FROM tracks t2 WHERE t2.album = t.album AND t2.artist = t.artist AND t2.has_cover = 1 ORDER BY t2.created_at LIMIT 1), '')
                FROM tracks t
                WHERE (t.album LIKE ? ESCAPE '\' OR t.artist LIKE ? ESCAPE '\')
                GROUP BY 1, 2 ORDER BY 1 COLLATE NOCASE ASC LIMIT 25`, like, like)
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

        rows3, err := s.db.Query(`
                SELECT COALESCE(NULLIF(artist,''),'Unknown Artist'), COUNT(*), COALESCE(SUM(duration),0)
                FROM tracks WHERE (artist LIKE ? ESCAPE '\' OR album LIKE ? ESCAPE '\')
                GROUP BY 1 ORDER BY 1 COLLATE NOCASE ASC LIMIT 25`, like, like)
        if err != nil {
                s.internalErr(w, "search artists", err)
                return
        }
        defer rows3.Close()
        artists := []*Artist{}
        for rows3.Next() {
                var a Artist
                if err := rows3.Scan(&a.Name, &a.TrackCount, &a.TotalDuration); err != nil {
                        s.internalErr(w, "scan search artist", err)
                        return
                }
                artists = append(artists, &a)
        }
        result["artists"] = artists

        // Uses playlistSelect so the selected columns match scanPlaylist, and so
        // search surfaces playlists other people have shared, not just your own.
        rows4, err := s.db.Query(playlistSelect+`
                WHERE (p.user_id = ? OR p.is_public = 1) AND p.name LIKE ? ESCAPE '\'
                ORDER BY p.created_at DESC LIMIT 25`, append(playlistSelectArgs(u.ID), u.ID, like)...)
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
                SELECT COUNT(*), COALESCE(SUM(duration),0), COUNT(DISTINCT COALESCE(NULLIF(artist,''),'Unknown Artist')), COUNT(DISTINCT COALESCE(NULLIF(album,''),'Unknown Album'))
                FROM tracks`).
                Scan(&trackCount, &totalDuration, &artistCount, &albumCount); err != nil {
                s.internalErr(w, "stats tracks", err)
                return
        }
        if err := s.db.QueryRow(`SELECT COUNT(1) FROM favorites WHERE user_id = ?`, u.ID).Scan(&favoriteCount); err != nil {
                s.internalErr(w, "stats favorites", err)
                return
        }
        if err := s.db.QueryRow(`SELECT COUNT(1) FROM playlists WHERE user_id = ?`, u.ID).Scan(&playlistCount); err != nil {
                s.internalErr(w, "stats playlists", err)
                return
        }

        // Listening time comes from the history, so it reflects every device.
        var listenedSeconds float64
        var listenCount int
        if err := s.db.QueryRow(`
                SELECT COUNT(1), COALESCE(SUM(t.duration), 0)
                FROM play_events pe JOIN tracks t ON t.id = pe.track_id
                WHERE pe.user_id = ?`, u.ID).Scan(&listenCount, &listenedSeconds); err != nil {
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
