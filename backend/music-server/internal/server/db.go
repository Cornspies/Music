package server

import (
	"database/sql"
	"fmt"

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

	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return db, nil
}

func createSchema(db *sql.DB) error {
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("%q: %w", s[:40], err)
		}
	}
	return nil
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS users (
                id            TEXT PRIMARY KEY,
                username      TEXT NOT NULL UNIQUE,
                password_hash TEXT NOT NULL,
                created_at    TEXT NOT NULL,
                has_avatar    INTEGER NOT NULL DEFAULT 0,
                token_version INTEGER NOT NULL DEFAULT 0,
                role          TEXT NOT NULL DEFAULT 'user'
        )`,
	`CREATE TABLE IF NOT EXISTS tracks (
                id                  TEXT PRIMARY KEY,
                user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                title               TEXT NOT NULL,
                artist              TEXT NOT NULL,
                album               TEXT NOT NULL,
                album_artist        TEXT NOT NULL DEFAULT '',
                genre               TEXT NOT NULL DEFAULT '',
                year                INTEGER NOT NULL DEFAULT 0,
                track_no            INTEGER NOT NULL DEFAULT 0,
                duration            REAL NOT NULL DEFAULT 0,
                file_name           TEXT NOT NULL,
                file_path           TEXT NOT NULL,
                mime_type           TEXT NOT NULL,
                file_size           INTEGER NOT NULL,
                file_hash           TEXT NOT NULL DEFAULT '',
                codec               TEXT NOT NULL DEFAULT '',
                bitrate             INTEGER NOT NULL DEFAULT 0,
                sample_rate         INTEGER NOT NULL DEFAULT 0,
                channels            INTEGER NOT NULL DEFAULT 0,
                bit_depth           INTEGER NOT NULL DEFAULT 0,
                alt_path            TEXT NOT NULL DEFAULT '',
                alt_mime            TEXT NOT NULL DEFAULT '',
                alt_size            INTEGER NOT NULL DEFAULT 0,
                has_cover           INTEGER NOT NULL DEFAULT 0,
                cover_source        TEXT NOT NULL DEFAULT '',
                cover_color         TEXT NOT NULL DEFAULT '',
                source              TEXT NOT NULL DEFAULT '',
                source_id           TEXT NOT NULL DEFAULT '',
                edges_checked       INTEGER NOT NULL DEFAULT 0,
                loudness            REAL,
                waveform_json       TEXT NOT NULL DEFAULT '',
                analyzed_at         TEXT NOT NULL DEFAULT '',
                attention_dismissed INTEGER NOT NULL DEFAULT 0,
                created_at          TEXT NOT NULL,
                updated_at          TEXT NOT NULL DEFAULT ''
        )`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_user ON tracks(user_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_created ON tracks(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_updated ON tracks(updated_at)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_source ON tracks(source, source_id)`,
	`CREATE INDEX IF NOT EXISTS idx_tracks_hash ON tracks(file_hash)`,
	`CREATE TABLE IF NOT EXISTS playlists (
                id            TEXT PRIMARY KEY,
                user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                name          TEXT NOT NULL,
                created_at    TEXT NOT NULL,
                is_public     INTEGER NOT NULL DEFAULT 0,
                cover_version TEXT NOT NULL DEFAULT ''
        )`,
	`CREATE TABLE IF NOT EXISTS playlist_tracks (
                playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
                track_id    TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                position    INTEGER NOT NULL,
                PRIMARY KEY (playlist_id, track_id)
        )`,
	`CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks(track_id)`,
	`CREATE TABLE IF NOT EXISTS play_stats (
                user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                track_id       TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                plays          INTEGER NOT NULL DEFAULT 0,
                last_played_at TEXT,
                PRIMARY KEY (user_id, track_id)
        )`,
	`CREATE INDEX IF NOT EXISTS idx_play_stats_user ON play_stats(user_id, plays DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_play_stats_track ON play_stats(track_id)`,
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
	`CREATE INDEX IF NOT EXISTS idx_favorites_track ON favorites(track_id)`,
	`CREATE TABLE IF NOT EXISTS playback_state (
                user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
                track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                position   REAL NOT NULL DEFAULT 0,
                queue_json TEXT NOT NULL DEFAULT '[]',
                updated_at TEXT NOT NULL
        )`,
	`CREATE TABLE IF NOT EXISTS play_events (
                id               INTEGER PRIMARY KEY AUTOINCREMENT,
                user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                track_id         TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
                played_at        TEXT NOT NULL,
                -- how much of the play was actually heard, reported by the
                -- client as it goes
                listened_seconds REAL NOT NULL DEFAULT 0
        )`,
	`CREATE INDEX IF NOT EXISTS idx_play_events_user ON play_events(user_id, played_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_play_events_track ON play_events(track_id)`,
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
	`CREATE TABLE IF NOT EXISTS artist_info (
                artist_key   TEXT PRIMARY KEY,
                name         TEXT NOT NULL DEFAULT '',
                deezer_id    INTEGER NOT NULL DEFAULT 0,
                picture      TEXT NOT NULL DEFAULT '',
                related_json TEXT NOT NULL DEFAULT '[]',
                fetched_at   TEXT NOT NULL DEFAULT ''
        )`,
	`CREATE TABLE IF NOT EXISTS library_pins (
                user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                kind       TEXT NOT NULL,
                name       TEXT NOT NULL,
                artist     TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                PRIMARY KEY (user_id, kind, name, artist)
        )`,
	`CREATE TABLE IF NOT EXISTS playback_prefs (
                user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                context    TEXT NOT NULL,
                shuffle    TEXT NOT NULL DEFAULT 'off',
                repeat     TEXT NOT NULL DEFAULT 'off',
                updated_at TEXT NOT NULL,
                PRIMARY KEY (user_id, context)
        )`,
	`CREATE TABLE IF NOT EXISTS user_prefs (
                user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                key        TEXT NOT NULL,
                value      TEXT NOT NULL,
                updated_at TEXT NOT NULL,
                PRIMARY KEY (user_id, key)
        )`,
	`CREATE TABLE IF NOT EXISTS password_resets (
                code_hash  TEXT PRIMARY KEY,
                user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
                created_at TEXT NOT NULL,
                expires_at TEXT NOT NULL
        )`,
	`CREATE INDEX IF NOT EXISTS idx_password_resets_user ON password_resets(user_id)`,
}
