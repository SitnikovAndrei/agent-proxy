package statistics

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ocm/internal/persistence"

	_ "modernc.org/sqlite"
)

const operationTimeout = 5 * time.Second

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database_path is required")
	}
	canonical, err := persistence.CanonicalPath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(canonical), 0o700); err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(canonical)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(3000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(FULL)")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.initialize(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return store, nil
}

func (s *Store) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	for _, view := range summaryViews {
		if _, err := tx.ExecContext(ctx, view); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Record(record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	return insertRecord(ctx, s.db, record)
}

func (s *Store) Close() error { return s.db.Close() }
