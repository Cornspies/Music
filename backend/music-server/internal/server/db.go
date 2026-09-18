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
        // A single connection serializes writes and avoids SQLITE_BUSY entirely.
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
        }
        for _, s := range stmts {
                if _, err := db.Exec(s); err != nil {
                        return fmt.Errorf("%q: %w", s[:40], err)
                }
        }

        // Idempotent column additions for existing databases (SQLite has no
        // "ADD COLUMN IF NOT EXISTS"): a duplicate-column error is expected
        // and ignored when the column is already present.
        addColumns := []struct{ ctx, stmt string }{
                {"tracks.last_played_at", `ALTER TABLE tracks ADD COLUMN last_played_at TEXT`},
                {"users.has_avatar", `ALTER TABLE users ADD COLUMN has_avatar INTEGER NOT NULL DEFAULT 0`},
                {"playlists.is_public", `ALTER TABLE playlists ADD COLUMN is_public INTEGER NOT NULL DEFAULT 0`},
                // A second, widely-playable copy of the audio, made at upload
                // time for formats some platforms can't decode (Ogg on iOS,
                // chiefly). The original is always kept and stays the default.
                {"tracks.alt_path", `ALTER TABLE tracks ADD COLUMN alt_path TEXT NOT NULL DEFAULT ''`},
                {"tracks.alt_mime", `ALTER TABLE tracks ADD COLUMN alt_mime TEXT NOT NULL DEFAULT ''`},
                {"tracks.alt_size", `ALTER TABLE tracks ADD COLUMN alt_size INTEGER NOT NULL DEFAULT 0`},
        }
        for _, c := range addColumns {
                if _, err := db.Exec(c.stmt); err != nil {
                        if !strings.Contains(err.Error(), "duplicate column name") {
                                return fmt.Errorf("add %s: %w", c.ctx, err)
                        }
                }
        }

        // The library is shared between users now, so the hot path is
        // "all tracks, newest first" rather than "this user's tracks".
        if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_tracks_created ON tracks(created_at DESC)`); err != nil {
                return fmt.Errorf("create idx_tracks_created: %w", err)
        }

        // Play counts used to live on tracks as a single shared number. They are
        // per-listener now, so seed each uploader's history from the old column.
        // INSERT OR IGNORE keeps this idempotent across restarts, and the legacy
        // columns are left untouched (read once here, never written again).
        if _, err := db.Exec(`
                INSERT OR IGNORE INTO play_stats (user_id, track_id, plays, last_played_at)
                SELECT user_id, id, plays, last_played_at FROM tracks
                WHERE plays > 0 OR (last_played_at IS NOT NULL AND last_played_at <> '')`); err != nil {
                return fmt.Errorf("backfill play_stats: %w", err)
        }
        return nil
}
