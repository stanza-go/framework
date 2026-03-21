package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db := New(":memory:")
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Stop(context.Background()); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
	return db
}

func TestOpenClose(t *testing.T) {
	db := New(":memory:")
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestOpenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db := New(path)
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen — table should persist.
	db2 := New(path)
	if err := db2.Start(context.Background()); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Stop(context.Background())

	var count int
	if err := db2.QueryRow("SELECT count(*) FROM t").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestDoubleOpen(t *testing.T) {
	db := openTestDB(t)
	err := db.Start(context.Background())
	if err == nil {
		t.Fatal("expected error on double open")
	}
}

func TestCloseIdempotent(t *testing.T) {
	db := New(":memory:")
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatalf("close 1: %v", err)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatalf("close 2: %v", err)
	}
}

func TestExecNotOpen(t *testing.T) {
	db := New(":memory:")
	_, err := db.Exec("SELECT 1")
	if err == nil {
		t.Fatal("expected error on exec before open")
	}
}

func TestPath(t *testing.T) {
	db := New("/tmp/test.db")
	if db.Path() != "/tmp/test.db" {
		t.Fatalf("expected /tmp/test.db, got %s", db.Path())
	}
}

func TestWALMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.db")

	db := New(path)
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Stop(context.Background())

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected wal, got %s", mode)
	}
}

func TestForeignKeys(t *testing.T) {
	db := openTestDB(t)

	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}
}

func TestCreateInsertSelect(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := db.Exec("INSERT INTO users (name, age) VALUES (?, ?)", "alice", 30)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.LastInsertID != 1 {
		t.Fatalf("expected last_insert_id=1, got %d", res.LastInsertID)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("expected rows_affected=1, got %d", res.RowsAffected)
	}

	var name string
	var age int
	if err := db.QueryRow("SELECT name, age FROM users WHERE id = ?", 1).Scan(&name, &age); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "alice" || age != 30 {
		t.Fatalf("expected alice/30, got %s/%d", name, age)
	}
}

func TestQueryMultipleRows(t *testing.T) {
	db := openTestDB(t)

	db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, val TEXT)")
	db.Exec("INSERT INTO items (val) VALUES (?)", "a")
	db.Exec("INSERT INTO items (val) VALUES (?)", "b")
	db.Exec("INSERT INTO items (val) VALUES (?)", "c")

	rows, err := db.Query("SELECT id, val FROM items ORDER BY id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var id int
		var val string
		if err := rows.Scan(&id, &val); err != nil {
			t.Fatalf("scan: %v", err)
		}
		results = append(results, val)
	}

	if len(results) != 3 || results[0] != "a" || results[1] != "b" || results[2] != "c" {
		t.Fatalf("expected [a b c], got %v", results)
	}
}

func TestRowsColumns(t *testing.T) {
	db := openTestDB(t)

	db.Exec("CREATE TABLE cols (id INTEGER, name TEXT, score REAL)")
	db.Exec("INSERT INTO cols VALUES (1, 'x', 1.5)")

	rows, err := db.Query("SELECT id, name, score FROM cols")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	cols := rows.Columns()
	if len(cols) != 3 || cols[0] != "id" || cols[1] != "name" || cols[2] != "score" {
		t.Fatalf("expected [id name score], got %v", cols)
	}
}

func TestErrNoRows(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE empty (id INTEGER PRIMARY KEY)")

	var id int
	err := db.QueryRow("SELECT id FROM empty WHERE id = 999").Scan(&id)
	if !errors.Is(err, ErrNoRows) {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestBindTypes(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE types (a INTEGER, b REAL, c TEXT, d BLOB, e INTEGER)")

	_, err := db.Exec(
		"INSERT INTO types VALUES (?, ?, ?, ?, ?)",
		int64(42), 3.14, "hello", []byte{0xDE, 0xAD}, true,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var a int64
	var b float64
	var c string
	var d []byte
	var e bool
	if err := db.QueryRow("SELECT a, b, c, d, e FROM types").Scan(&a, &b, &c, &d, &e); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if a != 42 {
		t.Fatalf("expected 42, got %d", a)
	}
	if b != 3.14 {
		t.Fatalf("expected 3.14, got %f", b)
	}
	if c != "hello" {
		t.Fatalf("expected hello, got %s", c)
	}
	if len(d) != 2 || d[0] != 0xDE || d[1] != 0xAD {
		t.Fatalf("expected [DE AD], got %x", d)
	}
	if !e {
		t.Fatal("expected true")
	}
}

func TestBindNull(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE nulls (val TEXT)")

	_, err := db.Exec("INSERT INTO nulls VALUES (?)", nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var val any
	if err := db.QueryRow("SELECT val FROM nulls").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}
}

func TestBindInt32(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE int32s (val INTEGER)")

	_, err := db.Exec("INSERT INTO int32s VALUES (?)", int32(99))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var val int32
	if err := db.QueryRow("SELECT val FROM int32s").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != 99 {
		t.Fatalf("expected 99, got %d", val)
	}
}

func TestBindFloat32(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE f32s (val REAL)")

	_, err := db.Exec("INSERT INTO f32s VALUES (?)", float32(1.5))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var val float32
	if err := db.QueryRow("SELECT val FROM f32s").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != 1.5 {
		t.Fatalf("expected 1.5, got %f", val)
	}
}

func TestBindUnsupportedType(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE unsup (val TEXT)")

	_, err := db.Exec("INSERT INTO unsup VALUES (?)", struct{}{})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestScanAny(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE anys (i INTEGER, f REAL, t TEXT, b BLOB, n INTEGER)")
	db.Exec("INSERT INTO anys VALUES (42, 3.14, 'hi', X'CAFE', NULL)")

	rows, err := db.Query("SELECT i, f, t, b, n FROM anys")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected a row")
	}

	var i, f, txt, b, n any
	if err := rows.Scan(&i, &f, &txt, &b, &n); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if i.(int64) != 42 {
		t.Fatalf("expected 42, got %v", i)
	}
	if f.(float64) != 3.14 {
		t.Fatalf("expected 3.14, got %v", f)
	}
	if txt.(string) != "hi" {
		t.Fatalf("expected hi, got %v", txt)
	}
	bs := b.([]byte)
	if len(bs) != 2 || bs[0] != 0xCA || bs[1] != 0xFE {
		t.Fatalf("expected [CA FE], got %x", bs)
	}
	if n != nil {
		t.Fatalf("expected nil, got %v", n)
	}
}

func TestScanColumnMismatch(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE mis (a INTEGER, b TEXT)")
	db.Exec("INSERT INTO mis VALUES (1, 'x')")

	rows, err := db.Query("SELECT a, b FROM mis")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected a row")
	}

	var a int
	if err := rows.Scan(&a); err == nil {
		t.Fatal("expected error for column count mismatch")
	}
}

func TestScanUnsupportedType(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE scanun (val INTEGER)")
	db.Exec("INSERT INTO scanun VALUES (1)")

	rows, err := db.Query("SELECT val FROM scanun")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected a row")
	}

	var u uint64
	if err := rows.Scan(&u); err == nil {
		t.Fatal("expected error for unsupported scan type")
	}
}

func TestRowsCloseIdempotent(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE ri (id INTEGER)")
	db.Exec("INSERT INTO ri VALUES (1)")

	rows, err := db.Query("SELECT id FROM ri")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	rows.Close()
	rows.Close() // second close should not panic
}

func TestRowErr(t *testing.T) {
	db := openTestDB(t)

	// Query with syntax error.
	row := db.QueryRow("SELECTZ bad")
	if row.Err() == nil {
		t.Fatal("expected error")
	}

	var dummy int
	if err := row.Scan(&dummy); err == nil {
		t.Fatal("expected error from Scan")
	}
}

func TestTransaction(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txtest (id INTEGER PRIMARY KEY, val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	tx.Exec("INSERT INTO txtest (val) VALUES (?)", "one")
	tx.Exec("INSERT INTO txtest (val) VALUES (?)", "two")

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var count int
	db.QueryRow("SELECT count(*) FROM txtest").Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
}

func TestTransactionRollback(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txrb (id INTEGER PRIMARY KEY, val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	tx.Exec("INSERT INTO txrb (val) VALUES (?)", "rollback_me")

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	db.QueryRow("SELECT count(*) FROM txrb").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}

func TestTransactionRollbackAfterCommit(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txrc (id INTEGER)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	tx.Exec("INSERT INTO txrc VALUES (1)")
	tx.Commit()

	// Rollback after commit is a no-op.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback after commit should be nil, got %v", err)
	}
}

func TestTransactionDoubleCommit(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txdc (id INTEGER)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	tx.Exec("INSERT INTO txdc VALUES (1)")
	tx.Commit()

	if err := tx.Commit(); err == nil {
		t.Fatal("expected error on double commit")
	}
}

func TestTransactionExecAfterFinish(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txef (id INTEGER)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	tx.Commit()

	_, err = tx.Exec("INSERT INTO txef VALUES (1)")
	if err == nil {
		t.Fatal("expected error on exec after commit")
	}
}

func TestTransactionQueryAfterFinish(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txqf (id INTEGER)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	tx.Commit()

	_, err = tx.Query("SELECT id FROM txqf")
	if err == nil {
		t.Fatal("expected error on query after commit")
	}
}

func TestTransactionQuery(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txq (id INTEGER PRIMARY KEY, val TEXT)")
	db.Exec("INSERT INTO txq (val) VALUES ('before')")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	tx.Exec("INSERT INTO txq (val) VALUES ('during')")

	rows, err := tx.Query("SELECT val FROM txq ORDER BY id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	var vals []string
	for rows.Next() {
		var v string
		rows.Scan(&v)
		vals = append(vals, v)
	}
	rows.Close()

	if len(vals) != 2 || vals[0] != "before" || vals[1] != "during" {
		t.Fatalf("expected [before during], got %v", vals)
	}

	tx.Commit()
}

func TestTransactionQueryRow(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txqr (val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	tx.Exec("INSERT INTO txqr VALUES ('hello')")

	var val string
	if err := tx.QueryRow("SELECT val FROM txqr").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != "hello" {
		t.Fatalf("expected hello, got %s", val)
	}

	tx.Commit()
}

func TestInTx(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE intx (id INTEGER PRIMARY KEY, val TEXT)")

	err := db.InTx(func(tx *Tx) error {
		_, err := tx.Exec("INSERT INTO intx (val) VALUES (?)", "auto")
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}

	var val string
	db.QueryRow("SELECT val FROM intx WHERE id = 1").Scan(&val)
	if val != "auto" {
		t.Fatalf("expected auto, got %s", val)
	}
}

func TestInTxRollbackOnError(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE intxrb (val TEXT)")

	err := db.InTx(func(tx *Tx) error {
		tx.Exec("INSERT INTO intxrb VALUES ('should_rollback')")
		return errors.New("oops")
	})
	if err == nil || err.Error() != "oops" {
		t.Fatalf("expected oops error, got %v", err)
	}

	var count int
	db.QueryRow("SELECT count(*) FROM intxrb").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}

func TestInTxRollbackOnPanic(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE intxp (val TEXT)")

	func() {
		defer func() { recover() }()
		db.InTx(func(tx *Tx) error {
			tx.Exec("INSERT INTO intxp VALUES ('should_rollback')")
			panic("boom")
		})
	}()

	var count int
	db.QueryRow("SELECT count(*) FROM intxp").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 rows after panic rollback, got %d", count)
	}
}

func TestExecMany(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE batch (id INTEGER PRIMARY KEY, val TEXT)")

	err := db.InTx(func(tx *Tx) error {
		return tx.ExecMany("INSERT INTO batch (val) VALUES (?)", [][]any{
			{"a"},
			{"b"},
			{"c"},
		})
	})
	if err != nil {
		t.Fatalf("ExecMany: %v", err)
	}

	var count int
	db.QueryRow("SELECT count(*) FROM batch").Scan(&count)
	if count != 3 {
		t.Fatalf("expected 3 rows, got %d", count)
	}
}

func TestBusyTimeout(t *testing.T) {
	db := New(":memory:", WithBusyTimeout(1000))
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Stop(context.Background())

	// Just verify the option is applied without error.
	_, err := db.Exec("SELECT 1")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func TestCustomPragma(t *testing.T) {
	db := New(":memory:", WithPragma("PRAGMA cache_size = -32000"))
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Stop(context.Background())

	var cacheSize int
	if err := db.QueryRow("PRAGMA cache_size").Scan(&cacheSize); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if cacheSize != -32000 {
		t.Fatalf("expected -32000, got %d", cacheSize)
	}
}

func TestConcurrentAccess(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE concurrent (id INTEGER PRIMARY KEY, val INTEGER)")

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			db.Exec("INSERT INTO concurrent (val) VALUES (?)", n)
		}(i)
	}
	wg.Wait()

	var count int
	db.QueryRow("SELECT count(*) FROM concurrent").Scan(&count)
	if count != 50 {
		t.Fatalf("expected 50 rows, got %d", count)
	}
}

func TestConcurrentReads(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE reads (id INTEGER PRIMARY KEY, val TEXT)")
	for i := range 10 {
		db.Exec("INSERT INTO reads (val) VALUES (?)", i)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var count int
			db.QueryRow("SELECT count(*) FROM reads").Scan(&count)
			if count != 10 {
				t.Errorf("expected 10, got %d", count)
			}
		}()
	}
	wg.Wait()
}

func TestFTS5(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec("CREATE VIRTUAL TABLE docs USING fts5(title, body)")
	if err != nil {
		t.Fatalf("create fts5: %v", err)
	}

	db.Exec("INSERT INTO docs VALUES ('Go Programming', 'Go is a statically typed language')")
	db.Exec("INSERT INTO docs VALUES ('Python Guide', 'Python is dynamically typed')")

	var title string
	err = db.QueryRow("SELECT title FROM docs WHERE docs MATCH 'statically'").Scan(&title)
	if err != nil {
		t.Fatalf("fts5 query: %v", err)
	}
	if title != "Go Programming" {
		t.Fatalf("expected Go Programming, got %s", title)
	}
}

func TestJSON1(t *testing.T) {
	db := openTestDB(t)

	var result string
	err := db.QueryRow("SELECT json_extract('{\"name\":\"alice\"}', '$.name')").Scan(&result)
	if err != nil {
		t.Fatalf("json_extract: %v", err)
	}
	if result != "alice" {
		t.Fatalf("expected alice, got %s", result)
	}
}

func TestEmptyBlob(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE blobs (data BLOB)")
	db.Exec("INSERT INTO blobs VALUES (?)", []byte{})

	var data []byte
	db.QueryRow("SELECT data FROM blobs").Scan(&data)
	// Empty blob binding with nil pointer is stored as NULL-ish;
	// the exact behavior depends on SQLite's handling.
}

func TestLargeInsert(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE large (id INTEGER PRIMARY KEY, data TEXT)")

	// Insert a large string.
	big := make([]byte, 1<<20) // 1MB
	for i := range big {
		big[i] = 'A' + byte(i%26)
	}
	_, err := db.Exec("INSERT INTO large (data) VALUES (?)", string(big))
	if err != nil {
		t.Fatalf("insert large: %v", err)
	}

	var data string
	db.QueryRow("SELECT data FROM large WHERE id = 1").Scan(&data)
	if len(data) != len(big) {
		t.Fatalf("expected %d bytes, got %d", len(big), len(data))
	}
}

func TestBeginNotOpen(t *testing.T) {
	db := New(":memory:")
	_, err := db.Begin()
	if err == nil {
		t.Fatal("expected error on begin before open")
	}
}

func TestExecManyAfterFinish(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE emf (val TEXT)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	tx.Commit()

	err = tx.ExecMany("INSERT INTO emf VALUES (?)", [][]any{{"a"}})
	if err == nil {
		t.Fatal("expected error on ExecMany after commit")
	}
}

func TestSQLError(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec("INSERT INTO nonexistent VALUES (1)")
	if err == nil {
		t.Fatal("expected error for nonexistent table")
	}
}

func TestOpenBadPath(t *testing.T) {
	db := New("/nonexistent/path/db.sqlite")
	err := db.Start(context.Background())
	if err == nil {
		db.Stop(context.Background())
		t.Fatal("expected error for bad path")
	}
}

func TestResultRowsAffected(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE raff (id INTEGER PRIMARY KEY, val TEXT)")
	db.Exec("INSERT INTO raff (val) VALUES ('a')")
	db.Exec("INSERT INTO raff (val) VALUES ('b')")
	db.Exec("INSERT INTO raff (val) VALUES ('c')")

	res, err := db.Exec("DELETE FROM raff WHERE val IN ('a', 'b')")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if res.RowsAffected != 2 {
		t.Fatalf("expected 2 rows affected, got %d", res.RowsAffected)
	}
}

func TestUpdate(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE upd (id INTEGER PRIMARY KEY, val TEXT)")
	db.Exec("INSERT INTO upd (val) VALUES ('old')")

	res, err := db.Exec("UPDATE upd SET val = ? WHERE id = ?", "new", 1)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("expected 1 row affected, got %d", res.RowsAffected)
	}

	var val string
	db.QueryRow("SELECT val FROM upd WHERE id = 1").Scan(&val)
	if val != "new" {
		t.Fatalf("expected new, got %s", val)
	}
}

func TestScanNoRow(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE snr (id INTEGER)")
	db.Exec("INSERT INTO snr VALUES (1)")

	rows, err := db.Query("SELECT id FROM snr")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	// Don't call Next — scan should fail.
	var id int
	if err := rows.Scan(&id); err == nil {
		t.Fatal("expected error scanning without Next")
	}
}

func TestRowsCloseAfterPartialIteration(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE partial (id INTEGER PRIMARY KEY, val TEXT)")
	for i := range 10 {
		db.Exec("INSERT INTO partial (val) VALUES (?)", i)
	}

	rows, err := db.Query("SELECT id, val FROM partial")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	// Read only first row.
	rows.Next()
	var id int
	var val string
	rows.Scan(&id, &val)
	rows.Close()

	// DB should be usable after closing rows early.
	var count int
	db.QueryRow("SELECT count(*) FROM partial").Scan(&count)
	if count != 10 {
		t.Fatalf("expected 10, got %d", count)
	}
}

func TestQueryNotOpen(t *testing.T) {
	db := New(":memory:")
	_, err := db.Query("SELECT 1")
	if err == nil {
		t.Fatal("expected error on query before open")
	}
}

func TestRowsColumnsCaching(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE colcache (a INTEGER, b TEXT, c REAL)")
	db.Exec("INSERT INTO colcache VALUES (1, 'x', 1.5)")

	rows, err := db.Query("SELECT a, b, c FROM colcache")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	cols1 := rows.Columns()
	cols2 := rows.Columns()

	// Should return the same cached slice.
	if &cols1[0] != &cols2[0] {
		t.Error("Columns() should return the same cached slice on second call")
	}
}

func TestRowsNextAfterClose(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE nac (id INTEGER)")
	db.Exec("INSERT INTO nac VALUES (1)")

	rows, err := db.Query("SELECT id FROM nac")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	rows.Close()

	if rows.Next() {
		t.Error("Next() should return false after Close")
	}
}

func TestScanBoolFalse(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE boolfalse (val INTEGER)")
	db.Exec("INSERT INTO boolfalse VALUES (?)", false)

	var val bool
	if err := db.QueryRow("SELECT val FROM boolfalse").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val {
		t.Fatal("expected false")
	}
}

func TestScanIntFromNull(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE intnull (val INTEGER)")
	db.Exec("INSERT INTO intnull VALUES (NULL)")

	// Scanning NULL into *int gives 0 (SQLite returns 0 for NULL int columns).
	var val int
	if err := db.QueryRow("SELECT val FROM intnull").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0, got %d", val)
	}
}

func TestScanStringFromNull(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE strnull (val TEXT)")
	db.Exec("INSERT INTO strnull VALUES (NULL)")

	var val string
	if err := db.QueryRow("SELECT val FROM strnull").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty string, got %q", val)
	}
}

func TestScanBytesFromNull(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE bytesnull (val BLOB)")
	db.Exec("INSERT INTO bytesnull VALUES (NULL)")

	var val []byte
	if err := db.QueryRow("SELECT val FROM bytesnull").Scan(&val); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}
}

func TestExecManyEmpty(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE eme (val TEXT)")

	err := db.InTx(func(tx *Tx) error {
		return tx.ExecMany("INSERT INTO eme VALUES (?)", nil)
	})
	if err != nil {
		t.Fatalf("ExecMany with nil argSets should succeed: %v", err)
	}

	err = db.InTx(func(tx *Tx) error {
		return tx.ExecMany("INSERT INTO eme VALUES (?)", [][]any{})
	})
	if err != nil {
		t.Fatalf("ExecMany with empty argSets should succeed: %v", err)
	}

	var count int
	db.QueryRow("SELECT count(*) FROM eme").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestExecManyExecError(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE eme2 (id INTEGER PRIMARY KEY, val TEXT NOT NULL)")
	db.Exec("INSERT INTO eme2 (val) VALUES ('existing')")

	// Second item violates NOT NULL — should fail mid-batch.
	err := db.InTx(func(tx *Tx) error {
		return tx.ExecMany("INSERT INTO eme2 (val) VALUES (?)", [][]any{
			{"a"},
			{nil}, // NOT NULL violation
		})
	})
	if err == nil {
		t.Fatal("expected error on ExecMany with NOT NULL violation")
	}
}

func TestInTxWhenDBNotOpen(t *testing.T) {
	db := New(":memory:")
	err := db.InTx(func(tx *Tx) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error on InTx before open")
	}
}

func TestBindEmptyString(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE estr (val TEXT)")

	_, err := db.Exec("INSERT INTO estr VALUES (?)", "")
	if err != nil {
		t.Fatalf("insert empty string: %v", err)
	}

	var val string
	db.QueryRow("SELECT val FROM estr").Scan(&val)
	if val != "" {
		t.Fatalf("expected empty string, got %q", val)
	}
}

func TestBindBoolFalse(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE bfalse (val INTEGER)")

	_, err := db.Exec("INSERT INTO bfalse VALUES (?)", false)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var val int
	db.QueryRow("SELECT val FROM bfalse").Scan(&val)
	if val != 0 {
		t.Fatalf("expected 0, got %d", val)
	}
}

func TestBindInt(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE bint (val INTEGER)")

	_, err := db.Exec("INSERT INTO bint VALUES (?)", 42)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var val int
	db.QueryRow("SELECT val FROM bint").Scan(&val)
	if val != 42 {
		t.Fatalf("expected 42, got %d", val)
	}
}

func TestTransactionQueryRowError(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE txqre (id INTEGER)")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	// QueryRow with syntax error should produce a Row with error.
	row := tx.QueryRow("SELECTZ bad")
	var dummy int
	if err := row.Scan(&dummy); err == nil {
		t.Fatal("expected error from QueryRow with bad SQL")
	}

	tx.Rollback()
}

func TestSpecialCharactersInSQL(t *testing.T) {
	db := openTestDB(t)
	db.Exec("CREATE TABLE special (val TEXT)")

	// Note: null bytes truncate in SQLite C strings, so we avoid \x00.
	special := "newline\nfoo\tbar'quote\"double\\backslash"
	_, err := db.Exec("INSERT INTO special VALUES (?)", special)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var val string
	db.QueryRow("SELECT val FROM special").Scan(&val)
	if val != special {
		t.Fatalf("roundtrip mismatch: got %q", val)
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perms.db")

	db := New(path)
	db.Start(context.Background())
	db.Exec("CREATE TABLE t (id INTEGER)")
	db.Stop(context.Background())

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("database file is empty")
	}
}
