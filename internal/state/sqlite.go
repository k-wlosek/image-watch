package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store against a SQLite database file.
type SQLiteStore struct {
	db *sql.DB
}

const createObservationsTable = `
CREATE TABLE IF NOT EXISTS observations (
	registry                  TEXT NOT NULL,
	repository                TEXT NOT NULL,
	tag                       TEXT NOT NULL,
	platform_os               TEXT NOT NULL DEFAULT '',
	platform_arch             TEXT NOT NULL DEFAULT '',
	platform_variant          TEXT NOT NULL DEFAULT '',
	platform_manifest_digest  TEXT NOT NULL DEFAULT '',
	index_digest              TEXT NOT NULL DEFAULT '',
	last_success              DATETIME,
	last_error                TEXT NOT NULL DEFAULT '',
	last_error_at             DATETIME,
	status                    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (registry, repository, tag, platform_os, platform_arch, platform_variant)
);
`

// NewSQLiteStore opens or creates a SQLite-backed Store at path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("state: failed to create state directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("state: failed to open sqlite database at %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(createObservationsTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("state: failed to initialize schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

var _ Store = (*SQLiteStore)(nil)

func (s *SQLiteStore) GetObservation(ctx context.Context, key Key) (Observation, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT platform_manifest_digest, index_digest, last_success, last_error, last_error_at, status
		FROM observations
		WHERE registry = ? AND repository = ? AND tag = ?
		  AND platform_os = ? AND platform_arch = ? AND platform_variant = ?
	`, key.Registry, key.Repository, key.Tag, key.Platform.OS, key.Platform.Architecture, key.Platform.Variant)

	var obs Observation
	obs.Key = key
	var lastSuccess, lastErrorAt sql.NullTime
	var status string

	err := row.Scan(&obs.PlatformManifestDigest, &obs.IndexDigest, &lastSuccess, &obs.LastError, &lastErrorAt, &status)
	if err == sql.ErrNoRows {
		return Observation{}, false, nil
	}
	if err != nil {
		return Observation{}, false, fmt.Errorf("state: failed to query observation: %w", err)
	}

	obs.Status = Status(status)
	if lastSuccess.Valid {
		obs.LastSuccess = lastSuccess.Time
	}
	if lastErrorAt.Valid {
		obs.LastErrorAt = lastErrorAt.Time
	}
	return obs, true, nil
}

func (s *SQLiteStore) PutObservation(ctx context.Context, obs Observation) error {
	var lastSuccess, lastErrorAt interface{}
	if !obs.LastSuccess.IsZero() {
		lastSuccess = obs.LastSuccess
	}
	if !obs.LastErrorAt.IsZero() {
		lastErrorAt = obs.LastErrorAt
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO observations (
			registry, repository, tag, platform_os, platform_arch, platform_variant,
			platform_manifest_digest, index_digest, last_success, last_error, last_error_at, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (registry, repository, tag, platform_os, platform_arch, platform_variant)
		DO UPDATE SET
			platform_manifest_digest = excluded.platform_manifest_digest,
			index_digest             = excluded.index_digest,
			last_success             = excluded.last_success,
			last_error               = excluded.last_error,
			last_error_at            = excluded.last_error_at,
			status                   = excluded.status
	`,
		obs.Key.Registry, obs.Key.Repository, obs.Key.Tag,
		obs.Key.Platform.OS, obs.Key.Platform.Architecture, obs.Key.Platform.Variant,
		obs.PlatformManifestDigest, obs.IndexDigest,
		lastSuccess, obs.LastError, lastErrorAt,
		string(obs.Status),
	)
	if err != nil {
		return fmt.Errorf("state: failed to persist observation: %w", err)
	}
	return nil
}
