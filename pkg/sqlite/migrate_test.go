package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateEmpty(t *testing.T) {
	db := openTestDB(t)
	n, err := db.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 migrations, got %d", n)
	}
}

func TestMigrateSingle(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
		return err
	}, nil)

	n, err := db.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 migration, got %d", n)
	}

	// Table should exist.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("query users: %v", err)
	}
}

func TestMigrateMultipleInOrder(t *testing.T) {
	db := openTestDB(t)

	// Register out of order — should still run in version order.
	db.AddMigration(1710892900, "create_posts", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id))")
		return err
	}, nil)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
		return err
	}, nil)

	n, err := db.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 migrations, got %d", n)
	}

	// Both tables should exist.
	if _, err := db.Exec("INSERT INTO users (name) VALUES (?)", "alice"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec("INSERT INTO posts (user_id) VALUES (?)", 1); err != nil {
		t.Fatalf("insert post: %v", err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	n1, err := db.Migrate()
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("expected 1, got %d", n1)
	}

	// Second call should be a no-op.
	n2, err := db.Migrate()
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0, got %d", n2)
	}
}

func TestMigrateRecordsInTable(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)
	db.AddMigration(1710892900, "create_posts", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Check _migrations table.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 records, got %d", count)
	}

	var version int64
	var name, appliedAt string
	err := db.QueryRow("SELECT version, name, applied_at FROM _migrations ORDER BY version LIMIT 1").Scan(&version, &name, &appliedAt)
	if err != nil {
		t.Fatalf("query row: %v", err)
	}
	if version != 1710892800 {
		t.Fatalf("expected version 1710892800, got %d", version)
	}
	if name != "create_users" {
		t.Fatalf("expected name create_users, got %s", name)
	}
	if appliedAt == "" {
		t.Fatal("expected applied_at to be set")
	}
}

func TestMigratePartialFailure(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)
	db.AddMigration(1710892900, "bad_migration", func(tx *Tx) error {
		_, err := tx.Exec("INVALID SQL HERE")
		return err
	}, nil)

	n, err := db.Migrate()
	if err == nil {
		t.Fatal("expected error from bad migration")
	}
	if n != 1 {
		t.Fatalf("expected 1 successful migration before failure, got %d", n)
	}

	// First migration should have been applied.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 applied migration, got %d", count)
	}

	// users table exists, but bad_migration's changes are rolled back.
	if err := db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("query users: %v", err)
	}
}

func TestMigrateFailedDoesNotRecord(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "bad_migration", func(tx *Tx) error {
		_, err := tx.Exec("INVALID SQL")
		return err
	}, nil)

	_, err := db.Migrate()
	if err == nil {
		t.Fatal("expected error")
	}

	// _migrations table should exist but be empty.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 records, got %d", count)
	}
}

func TestMigrateIncrementalAfterRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// First run: apply one migration.
	db1 := New(path)
	if err := db1.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	db1.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)
	n, err := db1.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
	if err := db1.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Second run: add a new migration, old one should be skipped.
	db2 := New(path)
	if err := db2.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer db2.Stop(context.Background())

	db2.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)
	db2.AddMigration(1710892900, "create_posts", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	n, err = db2.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 new migration, got %d", n)
	}
}

func TestRollback(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
		return err
	}, func(tx *Tx) error {
		_, err := tx.Exec("DROP TABLE users")
		return err
	})
	db.AddMigration(1710892900, "create_posts", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY)")
		return err
	}, func(tx *Tx) error {
		_, err := tx.Exec("DROP TABLE posts")
		return err
	})

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Rollback the latest (create_posts).
	v, err := db.Rollback()
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if v != 1710892900 {
		t.Fatalf("expected rolled back version 1710892900, got %d", v)
	}

	// posts table should be gone.
	_, err = db.Exec("SELECT 1 FROM posts")
	if err == nil {
		t.Fatal("expected error querying dropped table")
	}

	// users table should still exist.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("query users: %v", err)
	}

	// Only one migration record should remain.
	if err := db.QueryRow("SELECT count(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration record, got %d", count)
	}
}

func TestRollbackNoMigrations(t *testing.T) {
	db := openTestDB(t)
	v, err := db.Rollback()
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

func TestRollbackNoDownFunction(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, err := db.Rollback()
	if err == nil {
		t.Fatal("expected error when down is nil")
	}
}

func TestRollbackUnregisteredMigration(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, func(tx *Tx) error {
		_, err := tx.Exec("DROP TABLE users")
		return err
	})

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Create a new DB instance without the migration registered.
	db.migrations = nil

	_, err := db.Rollback()
	if err == nil {
		t.Fatal("expected error for unregistered migration")
	}
}

func TestAddMigrationPanicsZeroVersion(t *testing.T) {
	db := openTestDB(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero version")
		}
	}()
	db.AddMigration(0, "bad", func(tx *Tx) error { return nil }, nil)
}

func TestAddMigrationPanicsNilUp(t *testing.T) {
	db := openTestDB(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil up")
		}
	}()
	db.AddMigration(1, "bad", nil, nil)
}

func TestBackupBeforeMigrate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db := New(path)
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer db.Stop(context.Background())

	// Insert some data before migration so backup has content.
	if _, err := db.Exec("CREATE TABLE pre (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec("INSERT INTO pre (id) VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	backupPath := db.LastBackupPath()
	if backupPath == "" {
		t.Fatal("expected backup path to be set")
	}

	// Backup file should exist.
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("backup file is empty")
	}

	// Clean up backup.
	os.Remove(backupPath)
}

func TestBackupSkippedForMemory(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if db.LastBackupPath() != "" {
		t.Fatalf("expected no backup for in-memory db, got %s", db.LastBackupPath())
	}
}

func TestBackupSkippedWhenNoPendingMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db := New(path)
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer db.Stop(context.Background())

	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, nil)

	// First migrate creates a backup.
	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	firstBackup := db.LastBackupPath()
	os.Remove(firstBackup)

	// Reset to check second call does not create a new backup.
	db.lastBackupPath = ""

	// Second migrate — no pending migrations, no backup.
	n, err := db.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
	if db.LastBackupPath() != "" {
		t.Fatal("expected no backup when no pending migrations")
	}
}

func TestMigrateTransactionRollsBackOnFailure(t *testing.T) {
	db := openTestDB(t)

	db.AddMigration(1710892800, "partial_fail", func(tx *Tx) error {
		if _, err := tx.Exec("CREATE TABLE t1 (id INTEGER PRIMARY KEY)"); err != nil {
			return err
		}
		// This should fail and the whole transaction (including t1 creation) rolls back.
		_, err := tx.Exec("INVALID SQL")
		return err
	}, nil)

	_, err := db.Migrate()
	if err == nil {
		t.Fatal("expected error")
	}

	// t1 should not exist because the transaction was rolled back.
	_, err = db.Exec("SELECT 1 FROM t1")
	if err == nil {
		t.Fatal("expected error — table t1 should not exist after rollback")
	}
}

func TestRollbackMultipleSteps(t *testing.T) {
	db := openTestDB(t)
	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)")
		return err
	}, func(tx *Tx) error {
		_, err := tx.Exec("DROP TABLE users")
		return err
	})
	db.AddMigration(1710892900, "create_posts", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY)")
		return err
	}, func(tx *Tx) error {
		_, err := tx.Exec("DROP TABLE posts")
		return err
	})
	db.AddMigration(1710893000, "create_comments", func(tx *Tx) error {
		_, err := tx.Exec("CREATE TABLE comments (id INTEGER PRIMARY KEY)")
		return err
	}, func(tx *Tx) error {
		_, err := tx.Exec("DROP TABLE comments")
		return err
	})

	if _, err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Rollback all three, one at a time.
	versions := []int64{1710893000, 1710892900, 1710892800}
	for _, expected := range versions {
		v, err := db.Rollback()
		if err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if v != expected {
			t.Fatalf("expected %d, got %d", expected, v)
		}
	}

	// No more to rollback.
	v, err := db.Rollback()
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

func TestMigrateComplexSchema(t *testing.T) {
	db := openTestDB(t)

	db.AddMigration(1710892800, "create_users", func(tx *Tx) error {
		_, err := tx.Exec(`CREATE TABLE users (
			id         INTEGER PRIMARY KEY,
			email      TEXT    NOT NULL UNIQUE,
			name       TEXT    NOT NULL,
			active     INTEGER NOT NULL DEFAULT 1,
			created_at TEXT    NOT NULL DEFAULT (datetime('now'))
		)`)
		return err
	}, nil)

	db.AddMigration(1710892900, "create_sessions", func(tx *Tx) error {
		_, err := tx.Exec(`CREATE TABLE sessions (
			id         INTEGER PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id),
			token_hash TEXT    NOT NULL UNIQUE,
			expires_at TEXT    NOT NULL,
			created_at TEXT    NOT NULL DEFAULT (datetime('now'))
		)`)
		if err != nil {
			return err
		}
		_, err = tx.Exec("CREATE INDEX idx_sessions_user ON sessions(user_id)")
		return err
	}, nil)

	db.AddMigration(1710893000, "seed_admin", func(tx *Tx) error {
		_, err := tx.Exec("INSERT INTO users (email, name) VALUES (?, ?)", "admin@example.com", "Admin")
		return err
	}, nil)

	n, err := db.Migrate()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	// Verify the seeded data.
	var email string
	if err := db.QueryRow("SELECT email FROM users WHERE id = 1").Scan(&email); err != nil {
		t.Fatalf("query: %v", err)
	}
	if email != "admin@example.com" {
		t.Fatalf("expected admin@example.com, got %s", email)
	}
}
