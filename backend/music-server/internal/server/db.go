package server

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
                        id            TEXT PRIMARY KEY,
                        username      TEXT NOT NULL UNIQUE,
                        password_hash TEXT NOT NULL,
                        created_at    TEXT NOT NULL
                )`,
		`CREATE TABLE IF NOT EXISTS tracks (
                        id         TEXT PRIMARY KEY,
                        user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        title      TEXT NOT NULL,
                        artist     TEXT NOT NULL,
                        album      TEXT NOT NULL,
                        genre      TEXT NOT NULL DEFAULT '',
                        year       INTEGER NOT NULL DEFAULT 0,
                        track_no   INTEGER NOT NULL DEFAULT 0,
                        duration   REAL NOT NULL DEFAULT 0,
                        file_name  TEXT NOT NULL,
                        file_path  TEXT NOT NULL,
                        mime_type  TEXT NOT NULL,
                        file_size  INTEGER NOT NULL,
                        has_cover  INTEGER NOT NULL DEFAULT 0,
                        plays      INTEGER NOT NULL DEFAULT 0,
                        last_played_at TEXT,
                        created_at TEXT NOT NULL
                )`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_user ON tracks(user_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS playlists (
                        id         TEXT PRIMARY KEY,
                        user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        name       TEXT NOT NULL,
                        created_at TEXT NOT NULL
                )`,
		`CREATE TABLE IF NOT EXISTS playlist_tracks (
                        playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
                        track_id    TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                        position    INTEGER NOT NULL,
                        PRIMARY KEY (playlist_id, track_id)
                )`,
		`CREATE TABLE IF NOT EXISTS play_stats (
                        user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        track_id       TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                        plays          INTEGER NOT NULL DEFAULT 0,
                        last_played_at TEXT,
                        PRIMARY KEY (user_id, track_id)
                )`,
		`CREATE INDEX IF NOT EXISTS idx_play_stats_user ON play_stats(user_id, plays DESC)`,
		`CREATE TABLE IF NOT EXISTS playlist_pins (
                        user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
                        created_at  TEXT NOT NULL,
                        PRIMARY KEY (user_id, playlist_id)
                )`,
		`CREATE TABLE IF NOT EXISTS favorites (
                        user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                        created_at TEXT NOT NULL,
                        PRIMARY KEY (user_id, track_id)
                )`,
		`CREATE TABLE IF NOT EXISTS playback_state (
                        user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
                        track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                        position   REAL NOT NULL DEFAULT 0,
                        queue_json TEXT NOT NULL DEFAULT '[]',
                        updated_at TEXT NOT NULL
                )`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("%q: %w", s[:40], err)
		}
	}

	addColumns := []struct{ ctx, stmt string }{
		{"tracks.last_played_at", `ALTER TABLE tracks ADD COLUMN last_played_at TEXT`},
		{"users.has_avatar", `ALTER TABLE users ADD COLUMN has_avatar INTEGER NOT NULL DEFAULT 0`},
		{"playlists.is_public", `ALTER TABLE playlists ADD COLUMN is_public INTEGER NOT NULL DEFAULT 0`},
		{"tracks.alt_path", `ALTER TABLE tracks ADD COLUMN alt_path TEXT NOT NULL DEFAULT ''`},
		{"tracks.alt_mime", `ALTER TABLE tracks ADD COLUMN alt_mime TEXT NOT NULL DEFAULT ''`},
		{"tracks.alt_size", `ALTER TABLE tracks ADD COLUMN alt_size INTEGER NOT NULL DEFAULT 0`},
		{"users.token_version", `ALTER TABLE users ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0`},
		{"users.role", `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`},
		{"tracks.updated_at", `ALTER TABLE tracks ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`},
		{"tracks.source", `ALTER TABLE tracks ADD COLUMN source TEXT NOT NULL DEFAULT ''`},
		{"tracks.source_id", `ALTER TABLE tracks ADD COLUMN source_id TEXT NOT NULL DEFAULT ''`},
		{"tracks.cover_color", `ALTER TABLE tracks ADD COLUMN cover_color TEXT NOT NULL DEFAULT ''`},
		{"tracks.file_hash", `ALTER TABLE tracks ADD COLUMN file_hash TEXT NOT NULL DEFAULT ''`},
		{"tracks.edges_checked", `ALTER TABLE tracks ADD COLUMN edges_checked INTEGER NOT NULL DEFAULT 0`},
		{"tracks.loudness", `ALTER TABLE tracks ADD COLUMN loudness REAL`},
		{"tracks.waveform_json", `ALTER TABLE tracks ADD COLUMN waveform_json TEXT NOT NULL DEFAULT ''`},
		{"tracks.analyzed_at", `ALTER TABLE tracks ADD COLUMN analyzed_at TEXT NOT NULL DEFAULT ''`},
		{"tracks.cover_source", `ALTER TABLE tracks ADD COLUMN cover_source TEXT NOT NULL DEFAULT ''`},
		{"tracks.attention_dismissed", `ALTER TABLE tracks ADD COLUMN attention_dismissed INTEGER NOT NULL DEFAULT 0`},
	}
	for _, c := range addColumns {
		if _, err := db.Exec(c.stmt); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("add %s: %w", c.ctx, err)
			}
		}
	}

	later := []string{
		`CREATE TABLE IF NOT EXISTS play_events (
                        id        INTEGER PRIMARY KEY AUTOINCREMENT,
                        user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                        track_id  TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                        played_at TEXT NOT NULL,
                        -- how much of the play was actually heard, reported by
                        -- the client as it goes; reported = 0 marks rows from
                        -- before that existed (counted as the whole track)
                        listened_seconds REAL NOT NULL DEFAULT 0,
                        reported         INTEGER NOT NULL DEFAULT 0
                )`,
		`ALTER TABLE play_events ADD COLUMN listened_seconds REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE play_events ADD COLUMN reported INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_user ON play_events(user_id, played_at DESC)`,
		`CREATE TABLE IF NOT EXISTS deleted_tracks (
                        id         TEXT PRIMARY KEY,
                        deleted_at TEXT NOT NULL
                )`,
		`CREATE INDEX IF NOT EXISTS idx_deleted_tracks_at ON deleted_tracks(deleted_at)`,
		`CREATE TABLE IF NOT EXISTS favorite_changes (
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
			changed_at TEXT NOT NULL,
			PRIMARY KEY (user_id, track_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_favorite_changes_at ON favorite_changes(user_id, changed_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
                        key   TEXT PRIMARY KEY,
                        value TEXT NOT NULL
                )`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_updated ON tracks(updated_at)`,
		`CREATE TABLE IF NOT EXISTS lyrics (
                        track_id     TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
                        plain        TEXT NOT NULL DEFAULT '',
                        synced       TEXT NOT NULL DEFAULT '',
                        source       TEXT NOT NULL,
                        fetched_at   TEXT NOT NULL,
                        instrumental INTEGER NOT NULL DEFAULT 0
                )`,
		`CREATE TABLE IF NOT EXISTS metadata_jobs (
                        track_id        TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
                        state           TEXT NOT NULL,
                        reason          TEXT NOT NULL,
                        requested_by    TEXT NOT NULL DEFAULT '',
                        attempts        INTEGER NOT NULL DEFAULT 0,
                        not_before      TEXT NOT NULL DEFAULT '',
                        created_at      TEXT NOT NULL,
                        finished_at     TEXT NOT NULL DEFAULT '',
                        applied_id      TEXT NOT NULL DEFAULT '',
                        candidates_json TEXT NOT NULL DEFAULT '[]',
                        partial         INTEGER NOT NULL DEFAULT 0,
                        last_error      TEXT NOT NULL DEFAULT '',
                        cover_applied   INTEGER NOT NULL DEFAULT 0
                )`,
		`CREATE INDEX IF NOT EXISTS idx_metadata_jobs_state ON metadata_jobs(state, created_at)`,
		`ALTER TABLE metadata_jobs ADD COLUMN cover_applied INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS track_shares (
                        token      TEXT PRIMARY KEY,
                        track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                        created_by TEXT REFERENCES users(id) ON DELETE CASCADE,
                        created_at TEXT NOT NULL,
                        expires_at TEXT,
                        plays      INTEGER NOT NULL DEFAULT 0
                )`,
		`CREATE INDEX IF NOT EXISTS idx_track_shares_track ON track_shares(track_id)`,
		`CREATE TABLE IF NOT EXISTS invites (
                        code       TEXT PRIMARY KEY,
                        note       TEXT NOT NULL DEFAULT '',
                        created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
                        created_at TEXT NOT NULL,
                        expires_at TEXT NOT NULL,
                        used_by    TEXT REFERENCES users(id) ON DELETE SET NULL,
                        used_at    TEXT,
                        revoked_at TEXT
                )`,
	}
	for _, s := range later {
		if _, err := db.Exec(s); err != nil {
			if strings.HasPrefix(s, "ALTER TABLE") && strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("%q: %w", s[:40], err)
		}
	}
	if _, err := db.Exec(`UPDATE tracks SET updated_at = created_at WHERE updated_at = ''`); err != nil {
		return fmt.Errorf("seed updated_at: %w", err)
	}
	if _, err := db.Exec(`UPDATE tracks SET cover_source = CASE
			WHEN EXISTS (SELECT 1 FROM metadata_jobs j WHERE j.track_id = tracks.id AND (j.cover_applied = 1 OR j.applied_id <> '')) THEN 'match'
			ELSE 'thumbnail' END
		WHERE cover_source = '' AND source = 'youtube' AND has_cover = 1`); err != nil {
		return fmt.Errorf("seed cover_source: %w", err)
	}
	if _, err := db.Exec(`
                UPDATE users SET role = 'admin'
                WHERE NOT EXISTS (SELECT 1 FROM users WHERE role = 'admin')
                  AND id = (SELECT id FROM users WHERE id <> 'deleted-user' ORDER BY created_at ASC, id ASC LIMIT 1)`); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_tracks_created ON tracks(created_at DESC)`); err != nil {
		return fmt.Errorf("create idx_tracks_created: %w", err)
	}

	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks(track_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_stats_track ON play_stats(track_id)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_track ON play_events(track_id)`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_track ON favorites(track_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_source ON tracks(source, source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_hash ON tracks(file_hash)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create track fk index: %w", err)
		}
	}

	if _, err := db.Exec(`
                INSERT OR IGNORE INTO play_stats (user_id, track_id, plays, last_played_at)
                SELECT user_id, id, plays, last_played_at FROM tracks
                WHERE plays > 0 OR (last_played_at IS NOT NULL AND last_played_at <> '')`); err != nil {
		return fmt.Errorf("backfill play_stats: %w", err)
	}
	return nil
}
