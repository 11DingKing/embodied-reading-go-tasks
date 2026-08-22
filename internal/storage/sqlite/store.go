package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/migrations"
	_ "modernc.org/sqlite"
)

type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db *sql.DB
	q  queryer
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fault.Invalid("database_path", "must not be empty")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fault.Wrap(fault.Dependency, "database_open_failed", "open sqlite", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	store := &Store{db: db, q: db}
	if err := store.configure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) configure(ctx context.Context) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fault.Wrap(fault.Dependency, "database_config_failed", statement, err)
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.Wrap(fault.Dependency, "migration_begin_failed", "begin migration", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TEXT NOT NULL
    )`); err != nil {
		return fault.Wrap(fault.Dependency, "migration_table_failed", "create migration history", err)
	}
	var existing string
	err = tx.QueryRowContext(ctx, "SELECT name FROM schema_migrations WHERE version = 1").Scan(&existing)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, migrations.Initial); err != nil {
			return fault.Wrap(fault.Dependency, "migration_apply_failed", "apply initial schema", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, name, applied_at) VALUES(1, ?, ?)", "initial", formatTime(time.Now())); err != nil {
			return fault.Wrap(fault.Dependency, "migration_record_failed", "record migration", err)
		}
	case err != nil:
		return fault.Wrap(fault.Dependency, "migration_read_failed", "read migration history", err)
	case existing != "initial":
		return fault.New(fault.Conflict, "migration_history_conflict", "database contains a conflicting migration version")
	case existing == "initial":
		if err := tx.Commit(); err != nil {
			return fault.Wrap(fault.Dependency, "migration_commit_failed", "commit migration check", err)
		}
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fault.Wrap(fault.Dependency, "migration_commit_failed", "commit migration", err)
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fault.Wrap(fault.Dependency, "database_unavailable", "ping sqlite", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return mapContext(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.Wrap(fault.Dependency, "transaction_begin_failed", "begin transaction", err)
	}
	defer tx.Rollback()
	transactional := &Store{q: tx}
	if err := fn(ctx, transactional); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return mapContext(err)
	}
	if err := tx.Commit(); err != nil {
		return fault.Wrap(fault.Dependency, "transaction_commit_failed", "commit transaction", err)
	}
	return nil
}

func mapContext(err error) error {
	if errors.Is(err, context.Canceled) {
		return fault.Wrap(fault.Cancelled, "request_cancelled", "database operation", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fault.Wrap(fault.Deadline, "request_deadline", "database operation", err)
	}
	return err
}

func mapSQLError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fault.Wrap(fault.NotFound, "record_not_found", operation, err)
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fault.Wrap(fault.Conflict, "unique_constraint", operation, err)
	}
	if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		return fault.Wrap(fault.Precondition, "foreign_key_constraint", operation, err)
	}
	return fault.Wrap(fault.Dependency, "database_operation_failed", operation, err)
}

func requireUpdated(result sql.Result, operation string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return mapSQLError(operation+" rows affected", err)
	}
	if count != 1 {
		return fault.New(fault.Conflict, "version_conflict", fmt.Sprintf("%s lost optimistic ownership", operation))
	}
	return nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fault.Wrap(fault.Dependency, "invalid_persisted_time", "parse persisted timestamp", err)
	}
	return parsed.UTC(), nil
}

func optionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

func optionalTimePointer(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func scanOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
