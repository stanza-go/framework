// Package sqlite provides a SQLite database driver built from the vendored
// amalgamation via CGo, with read/write connection separation, a type-safe
// query builder, schema migrations, and structured error handling.
//
// The driver links directly against sqlite3.c — no database/sql, no
// third-party driver. A single write connection serializes mutations while
// a pool of read connections (default 4) serves concurrent queries.
//
// Opening a database:
//
//	db := sqlite.New("data.db",
//	    sqlite.WithReadPoolSize(4),
//	    sqlite.WithBusyTimeout(5000),
//	    sqlite.WithLogger(logger),
//	)
//	defer db.Close()
//
// Query builder:
//
//	rows, err := db.Query(sqlite.Select("id", "name").
//	    From("users").
//	    Where("active = ?", true).
//	    OrderBy("name ASC").
//	    Limit(20).
//	    Build())
//
//	result, err := db.Exec(sqlite.Insert("users").
//	    Set("name", "Alice").
//	    Set("email", "alice@example.com").
//	    Build())
//
// Transactions:
//
//	err := db.InTx(func(tx *sqlite.Tx) error {
//	    tx.Exec(sqlite.Update("accounts").Set("balance", newBalance).Where("id = ?", id).Build())
//	    return nil
//	})
//
// Migrations run automatically on boot:
//
//	db.Migrate([]sqlite.Migration{
//	    {Name: "1710892800_create_users", SQL: createUsersSQL},
//	})
//
// Error handling provides typed errors with SQLite result codes:
//
//	if sqlite.IsUniqueConstraintError(err) {
//	    // handle duplicate
//	}
package sqlite
