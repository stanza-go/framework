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
// # Query builder — executing queries
//
// Every builder has an execution method that accepts DB or Tx. This is the
// recommended way to run builder queries:
//
//	// SELECT — use Query or QueryRow
//	rows, err := sqlite.Select("id", "name").From("users").
//	    Where("active = ?", true).
//	    OrderBy("name ASC").
//	    Limit(20).
//	    Query(db)
//
//	// INSERT — use Exec, returns last insert ID
//	id, err := sqlite.Insert("users").
//	    Set("name", "Alice").
//	    Set("email", "alice@example.com").
//	    Exec(db)
//
//	// UPDATE — use Exec, returns rows affected
//	n, err := sqlite.Update("users").
//	    Set("name", "Bob").
//	    Where("id = ?", id).
//	    Exec(db)
//
//	// DELETE — use Exec, returns rows affected
//	n, err := sqlite.Delete("users").Where("id = ?", id).Exec(db)
//
//	// COUNT — use Count
//	total, err := sqlite.Count("users").Where("active = ?", true).Count(db)
//
// The DB convenience methods (db.Insert, db.Update, db.Delete, db.Count)
// also work correctly:
//
//	id, err := db.Insert(sqlite.Insert("users").Set("name", "Alice"))
//
// WARNING: Do NOT pass builder.Build() directly to db.Exec or db.Query:
//
//	// BUG — silently loses query parameters!
//	db.Exec(sqlite.Update("users").Set("name", "Bob").Where("id = ?", 1).Build())
//
// Build() returns (string, []any). When passed via multi-return forwarding,
// Go wraps the []any as a single variadic element instead of spreading it.
// The query runs with wrong parameters and silently affects zero rows.
// Always use builder.Exec(db), builder.Query(db), or the db.Update(builder)
// convenience methods instead.
//
// # Transactions
//
// Builder execution methods work with both DB and Tx:
//
//	err := db.InTx(func(tx *sqlite.Tx) error {
//	    _, err := sqlite.Update("accounts").
//	        Set("balance", newBalance).
//	        Where("id = ?", id).
//	        Exec(tx)
//	    return err
//	})
//
// # Migrations
//
// Migrations run automatically on boot:
//
//	db.Migrate([]sqlite.Migration{
//	    {Name: "1710892800_create_users", SQL: createUsersSQL},
//	})
//
// # Error handling
//
// Typed errors with SQLite result codes:
//
//	if sqlite.IsUniqueConstraintError(err) {
//	    // handle duplicate
//	}
package sqlite
