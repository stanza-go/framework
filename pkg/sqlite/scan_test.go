package sqlite

import (
	"errors"
	"testing"
)

func TestQueryAll_Basic(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO items (name) VALUES (?), (?), (?)", "a", "b", "c"); err != nil {
		t.Fatal(err)
	}

	type item struct {
		ID   int64
		Name string
	}

	items, err := QueryAll(db, "SELECT id, name FROM items ORDER BY id", nil, func(rows *Rows) (item, error) {
		var it item
		err := rows.Scan(&it.ID, &it.Name)
		return it, err
	})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[0].Name != "a" || items[1].Name != "b" || items[2].Name != "c" {
		t.Errorf("items = %v, want a/b/c", items)
	}
}

func TestQueryAll_EmptyResult(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("CREATE TABLE empty (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	items, err := QueryAll(db, "SELECT id FROM empty", nil, func(rows *Rows) (int64, error) {
		var id int64
		err := rows.Scan(&id)
		return id, err
	})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if items == nil {
		t.Fatal("got nil slice, want non-nil empty slice")
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestQueryAll_WithArgs(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT, active INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO products (name, active) VALUES (?, ?), (?, ?), (?, ?)", "x", 1, "y", 0, "z", 1); err != nil {
		t.Fatal(err)
	}

	names, err := QueryAll(db, "SELECT name FROM products WHERE active = ?", []any{1}, func(rows *Rows) (string, error) {
		var name string
		err := rows.Scan(&name)
		return name, err
	})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
}

func TestQueryAll_ScanError(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("CREATE TABLE nums (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO nums (val) VALUES (?)", "hello"); err != nil {
		t.Fatal(err)
	}

	scanErr := errors.New("scan failed")
	_, err := QueryAll(db, "SELECT id, val FROM nums", nil, func(rows *Rows) (int, error) {
		return 0, scanErr
	})
	if !errors.Is(err, scanErr) {
		t.Fatalf("got err %v, want %v", err, scanErr)
	}
}

func TestQueryAll_BadSQL(t *testing.T) {
	db := openTestDB(t)

	_, err := QueryAll(db, "SELECT * FROM nonexistent", nil, func(rows *Rows) (int, error) {
		return 0, nil
	})
	if err == nil {
		t.Fatal("expected error for bad SQL")
	}
}

func TestQueryAll_WithBuilder(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT, active INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users (email, active) VALUES (?, ?), (?, ?)", "a@b.com", 1, "c@d.com", 0); err != nil {
		t.Fatal(err)
	}

	type user struct {
		ID    int64
		Email string
	}

	sql, args := Select("id", "email").
		From("users").
		Where("active = ?", 1).
		OrderBy("id", "ASC").
		Build()

	users, err := QueryAll(db, sql, args, func(rows *Rows) (user, error) {
		var u user
		err := rows.Scan(&u.ID, &u.Email)
		return u, err
	})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}
	if users[0].Email != "a@b.com" {
		t.Errorf("email = %q, want %q", users[0].Email, "a@b.com")
	}
}

func TestQueryAll_SingleRow(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("CREATE TABLE singles (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO singles (val) VALUES (?)", "only"); err != nil {
		t.Fatal(err)
	}

	items, err := QueryAll(db, "SELECT val FROM singles", nil, func(rows *Rows) (string, error) {
		var val string
		err := rows.Scan(&val)
		return val, err
	})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(items) != 1 || items[0] != "only" {
		t.Errorf("items = %v, want [only]", items)
	}
}
