package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func openBenchDB(b *testing.B) *DB {
	b.Helper()
	dir := b.TempDir()
	db := New(filepath.Join(dir, "bench.db"), WithReadPoolSize(4))
	if err := db.Start(context.Background()); err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { db.Stop(context.Background()) })
	return db
}

func seedBenchTable(b *testing.B, db *DB, n int) {
	b.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		b.Fatalf("create table: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	argSets := make([][]any, n)
	for i := range n {
		argSets[i] = []any{fmt.Sprintf("user_%d", i), fmt.Sprintf("user_%d@example.com", i)}
	}
	if err := tx.ExecMany("INSERT INTO users (name, email) VALUES (?, ?)", argSets); err != nil {
		tx.Rollback()
		b.Fatalf("seed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit: %v", err)
	}
}

// --- SQLite read/write operations ---

func BenchmarkExec_Insert(b *testing.B) {
	db := openBenchDB(b)
	if _, err := db.Exec("CREATE TABLE bench (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		b.Fatalf("create: %v", err)
	}

	b.ResetTimer()
	for i := range b.N {
		if _, err := db.Exec("INSERT INTO bench (val) VALUES (?)", fmt.Sprintf("val_%d", i)); err != nil {
			b.Fatalf("insert: %v", err)
		}
	}
}

func BenchmarkQueryRow_ByPK(b *testing.B) {
	db := openBenchDB(b)
	seedBenchTable(b, db, 10000)

	b.ResetTimer()
	var name string
	for i := range b.N {
		id := (i % 10000) + 1
		if err := db.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&name); err != nil {
			b.Fatalf("query: %v", err)
		}
	}
}

func BenchmarkQuery_Paginated(b *testing.B) {
	db := openBenchDB(b)
	seedBenchTable(b, db, 10000)

	b.ResetTimer()
	for i := range b.N {
		offset := (i * 20) % 10000
		rows, err := db.Query("SELECT id, name, email FROM users ORDER BY id LIMIT 20 OFFSET ?", offset)
		if err != nil {
			b.Fatalf("query: %v", err)
		}
		var id int
		var name, email string
		for rows.Next() {
			if err := rows.Scan(&id, &name, &email); err != nil {
				b.Fatalf("scan: %v", err)
			}
		}
		rows.Close()
	}
}

func BenchmarkInTx_ReadWrite(b *testing.B) {
	db := openBenchDB(b)
	if _, err := db.Exec("CREATE TABLE bench (id INTEGER PRIMARY KEY, counter INTEGER DEFAULT 0)"); err != nil {
		b.Fatalf("create: %v", err)
	}
	if _, err := db.Exec("INSERT INTO bench (id, counter) VALUES (1, 0)"); err != nil {
		b.Fatalf("seed: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		if err := db.InTx(func(tx *Tx) error {
			var counter int
			if err := tx.QueryRow("SELECT counter FROM bench WHERE id = 1").Scan(&counter); err != nil {
				return err
			}
			_, err := tx.Exec("UPDATE bench SET counter = ? WHERE id = 1", counter+1)
			return err
		}); err != nil {
			b.Fatalf("tx: %v", err)
		}
	}
}

// --- Concurrent read/write contention ---

func BenchmarkConcurrentReads(b *testing.B) {
	db := openBenchDB(b)
	seedBenchTable(b, db, 10000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		var name string
		for pb.Next() {
			id := (i % 10000) + 1
			if err := db.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&name); err != nil {
				b.Errorf("query: %v", err)
				return
			}
			i++
		}
	})
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	db := openBenchDB(b)
	seedBenchTable(b, db, 1000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				// 10% writes
				if _, err := db.Exec("UPDATE users SET status = ? WHERE id = ?", "updated", (i%1000)+1); err != nil {
					b.Errorf("write: %v", err)
					return
				}
			} else {
				// 90% reads
				var name string
				if err := db.QueryRow("SELECT name FROM users WHERE id = ?", (i%1000)+1).Scan(&name); err != nil {
					b.Errorf("read: %v", err)
					return
				}
			}
			i++
		}
	})
}

func BenchmarkPoolContention(b *testing.B) {
	db := openBenchDB(b)
	seedBenchTable(b, db, 1000)

	// Hammer with more goroutines than pool size (4) to force waits
	b.SetParallelism(8)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		var name string
		for pb.Next() {
			id := (i % 1000) + 1
			if err := db.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&name); err != nil {
				b.Errorf("query: %v", err)
				return
			}
			i++
		}
	})
}

// --- Query builder SQL generation ---

func BenchmarkBuilder_SimpleSelect(b *testing.B) {
	for range b.N {
		Select("id", "name", "email").
			From("users").
			Where("deleted_at IS NULL").
			Where("is_active = ?", 1).
			OrderBy("id", "DESC").
			Limit(20).
			Offset(0).
			Build()
	}
}

func BenchmarkBuilder_ComplexSelect(b *testing.B) {
	for range b.N {
		Select("u.id", "u.name", "u.email", "r.name AS role").
			From("users u").
			LeftJoin("roles r", "r.id = u.role_id").
			Where("u.deleted_at IS NULL").
			WhereOr(Cond("u.name LIKE ?", "%test%"), Cond("u.email LIKE ?", "%test%")).
			OrderBy("u.created_at", "DESC").
			Limit(20).
			Offset(40).
			Build()
	}
}

func BenchmarkBuilder_Insert(b *testing.B) {
	for range b.N {
		Insert("users").
			Set("name", "John Doe").
			Set("email", "john@example.com").
			Set("status", "active").
			Build()
	}
}

func BenchmarkBuilder_Update(b *testing.B) {
	for range b.N {
		Update("users").
			Set("name", "Jane Doe").
			Set("status", "inactive").
			Where("id = ?", 42).
			Where("deleted_at IS NULL").
			Build()
	}
}

func BenchmarkBuilder_Delete(b *testing.B) {
	for range b.N {
		Delete("users").
			Where("deleted_at IS NOT NULL").
			Where("updated_at < ?", "2024-01-01").
			Build()
	}
}

// --- Batch operations ---

func BenchmarkExecMany_BatchInsert(b *testing.B) {
	db := openBenchDB(b)
	if _, err := db.Exec("CREATE TABLE bench (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		b.Fatalf("create: %v", err)
	}

	batchSize := 100
	argSets := make([][]any, batchSize)
	for i := range batchSize {
		argSets[i] = []any{fmt.Sprintf("val_%d", i)}
	}

	b.ResetTimer()
	for range b.N {
		tx, err := db.Begin()
		if err != nil {
			b.Fatalf("begin: %v", err)
		}
		if err := tx.ExecMany("INSERT INTO bench (val) VALUES (?)", argSets); err != nil {
			tx.Rollback()
			b.Fatalf("batch: %v", err)
		}
		tx.Commit()
	}
}

// --- Write contention (multiple writers) ---

func BenchmarkWriteContention(b *testing.B) {
	db := openBenchDB(b)
	if _, err := db.Exec("CREATE TABLE bench (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		b.Fatalf("create: %v", err)
	}

	var counter int64
	b.ResetTimer()

	var wg sync.WaitGroup
	goroutines := 8
	perGoroutine := b.N / goroutines
	if perGoroutine == 0 {
		perGoroutine = 1
	}

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGoroutine {
				n := fmt.Sprintf("val_%d", counter)
				counter++
				if _, err := db.Exec("INSERT INTO bench (val) VALUES (?)", n); err != nil {
					b.Errorf("insert: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
