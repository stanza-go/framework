package sqlite

// QueryAll executes a query and scans all result rows using the
// provided function. It handles the full lifecycle: query execution,
// iteration, row scanning, error checking, and resource cleanup.
//
// The scan function is called once per row and should only call
// rows.Scan to read column values into the returned value.
//
// QueryAll always returns a non-nil slice (empty when no rows match),
// which serializes to [] in JSON instead of null.
//
//	sql, args := sqlite.Select("id", "email", "name").
//		From("users").
//		Where("deleted_at IS NULL").
//		OrderBy("id", "ASC").
//		Build()
//	users, err := sqlite.QueryAll(db, sql, args, func(rows *sqlite.Rows) (User, error) {
//		var u User
//		err := rows.Scan(&u.ID, &u.Email, &u.Name)
//		return u, err
//	})
func QueryAll[T any](db *DB, sql string, args []any, scan func(*Rows) (T, error)) ([]T, error) {
	rows, err := db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
