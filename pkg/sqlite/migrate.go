package sqlite

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Migration represents a single database migration. Version is a Unix
// timestamp that determines execution order. Up applies the migration
// and Down reverses it, both running inside a transaction.
type Migration struct {
	Version int64
	Name    string
	Up      func(tx *Tx) error
	Down    func(tx *Tx) error
}

// AddMigration registers a migration. Migrations are sorted by version
// before execution, so registration order does not matter. Panics if
// version is zero or Up is nil.
func (db *DB) AddMigration(version int64, name string, up, down func(tx *Tx) error) {
	if version == 0 {
		panic("sqlite: migration version must not be zero")
	}
	if up == nil {
		panic("sqlite: migration up function must not be nil")
	}
	db.migrations = append(db.migrations, Migration{
		Version: version,
		Name:    name,
		Up:      up,
		Down:    down,
	})
}

// Migrate runs all pending migrations in version order. Each migration
// runs in its own transaction — if a migration fails, previously
// applied migrations in this call are NOT rolled back. For file-backed
// databases, the SQLite file is copied to a temporary directory before
// any migrations run.
//
// Returns the number of migrations applied and any error encountered.
func (db *DB) Migrate() (int, error) {
	if len(db.migrations) == 0 {
		return 0, nil
	}

	if err := db.ensureMigrationsTable(); err != nil {
		return 0, fmt.Errorf("sqlite: migrate: %w", err)
	}

	applied, err := db.appliedVersions()
	if err != nil {
		return 0, fmt.Errorf("sqlite: migrate: %w", err)
	}

	pending := db.pendingMigrations(applied)
	if len(pending) == 0 {
		return 0, nil
	}

	if err := db.backupBeforeMigrate(); err != nil {
		return 0, fmt.Errorf("sqlite: migrate: backup: %w", err)
	}

	count := 0
	for _, m := range pending {
		if err := db.runMigration(m); err != nil {
			return count, fmt.Errorf("sqlite: migrate %d_%s: %w", m.Version, m.Name, err)
		}
		count++
	}

	return count, nil
}

// Rollback reverses the last applied migration. Returns the version
// that was rolled back, or 0 if there are no migrations to reverse.
func (db *DB) Rollback() (int64, error) {
	if err := db.ensureMigrationsTable(); err != nil {
		return 0, fmt.Errorf("sqlite: rollback: %w", err)
	}

	var version int64
	var name string
	err := db.QueryRow("SELECT version, name FROM _migrations ORDER BY version DESC LIMIT 1").Scan(&version, &name)
	if err == ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("sqlite: rollback: %w", err)
	}

	m, ok := db.findMigration(version)
	if !ok {
		return 0, fmt.Errorf("sqlite: rollback: migration %d_%s not registered", version, name)
	}
	if m.Down == nil {
		return 0, fmt.Errorf("sqlite: rollback: migration %d_%s has no down function", version, name)
	}

	if err := db.InTx(func(tx *Tx) error {
		if err := m.Down(tx); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM _migrations WHERE version = ?", version)
		return err
	}); err != nil {
		return 0, fmt.Errorf("sqlite: rollback %d_%s: %w", version, name, err)
	}

	return version, nil
}

// ensureMigrationsTable creates the _migrations table if it does not exist.
func (db *DB) ensureMigrationsTable() error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at TEXT    NOT NULL
	)`)
	return err
}

// appliedVersions returns the set of already-applied migration versions.
func (db *DB) appliedVersions() (map[int64]bool, error) {
	rows, err := db.Query("SELECT version FROM _migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int64]bool)
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

// pendingMigrations returns registered migrations not yet applied,
// sorted by version ascending.
func (db *DB) pendingMigrations(applied map[int64]bool) []Migration {
	var pending []Migration
	for _, m := range db.migrations {
		if !applied[m.Version] {
			pending = append(pending, m)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Version < pending[j].Version
	})
	return pending
}

// runMigration applies a single migration inside a transaction and
// records it in the _migrations table.
func (db *DB) runMigration(m Migration) error {
	return db.InTx(func(tx *Tx) error {
		if err := m.Up(tx); err != nil {
			return err
		}
		_, err := tx.Exec(
			"INSERT INTO _migrations (version, name, applied_at) VALUES (?, ?, ?)",
			m.Version, m.Name, time.Now().UTC().Format(time.RFC3339),
		)
		return err
	})
}

// findMigration looks up a registered migration by version.
func (db *DB) findMigration(version int64) (Migration, bool) {
	for _, m := range db.migrations {
		if m.Version == version {
			return m, true
		}
	}
	return Migration{}, false
}

// backupBeforeMigrate copies the database file to a temporary directory.
// Skipped for in-memory databases. The backup path is printed to stderr
// for visibility.
func (db *DB) backupBeforeMigrate() error {
	if db.path == ":memory:" || db.path == "" {
		return nil
	}

	src, err := os.Open(db.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // new database, nothing to back up
		}
		return err
	}
	defer src.Close()

	base := filepath.Base(db.path)
	ts := time.Now().UTC().Format("20060102T150405Z")
	backupName := fmt.Sprintf("%s.%s.bak", base, ts)
	backupPath := filepath.Join(os.TempDir(), backupName)

	dst, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(backupPath)
		return err
	}

	db.mu.Lock()
	db.lastBackupPath = backupPath
	db.mu.Unlock()
	return nil
}

// LastBackupPath returns the path of the most recent pre-migration
// backup, or an empty string if no backup was made.
func (db *DB) LastBackupPath() string {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.lastBackupPath
}
