package sqlite

import (
	"reflect"
	"testing"
)

func TestSelect_Basic(t *testing.T) {
	sql, args := Select("id", "name", "email").
		From("users").
		Build()

	wantSQL := "SELECT id, name, email FROM users"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestSelect_Where(t *testing.T) {
	sql, args := Select("id", "email").
		From("users").
		Where("deleted_at IS NULL").
		Where("is_active = ?", 1).
		Build()

	wantSQL := "SELECT id, email FROM users WHERE deleted_at IS NULL AND is_active = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{1}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_OrderBy(t *testing.T) {
	sql, args := Select("id", "name").
		From("users").
		Where("deleted_at IS NULL").
		OrderBy("id", "DESC").
		Build()

	wantSQL := "SELECT id, name FROM users WHERE deleted_at IS NULL ORDER BY id DESC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestSelect_MultipleOrderBy(t *testing.T) {
	sql, _ := Select("key", "value").
		From("settings").
		OrderBy("group_name", "ASC").
		OrderBy("key", "ASC").
		Build()

	wantSQL := "SELECT key, value FROM settings ORDER BY group_name ASC, key ASC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
}

func TestSelect_LimitOffset(t *testing.T) {
	sql, args := Select("id", "name").
		From("users").
		Where("deleted_at IS NULL").
		OrderBy("id", "ASC").
		Limit(10).
		Offset(20).
		Build()

	wantSQL := "SELECT id, name FROM users WHERE deleted_at IS NULL ORDER BY id ASC LIMIT ? OFFSET ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{10, 20}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_LeftJoin(t *testing.T) {
	sql, args := Select("rt.id", "rt.entity_type", "COALESCE(a.email, '')").
		From("refresh_tokens rt").
		LeftJoin("admins a", "rt.entity_type = 'admin' AND rt.entity_id = CAST(a.id AS TEXT)").
		Where("rt.expires_at > ?", "2026-01-01T00:00:00Z").
		OrderBy("rt.created_at", "DESC").
		Build()

	wantSQL := "SELECT rt.id, rt.entity_type, COALESCE(a.email, '') FROM refresh_tokens rt LEFT JOIN admins a ON rt.entity_type = 'admin' AND rt.entity_id = CAST(a.id AS TEXT) WHERE rt.expires_at > ? ORDER BY rt.created_at DESC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2026-01-01T00:00:00Z"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_Join(t *testing.T) {
	sql, _ := Select("u.id", "r.name").
		From("users u").
		Join("roles r", "u.role_id = r.id").
		Build()

	wantSQL := "SELECT u.id, r.name FROM users u JOIN roles r ON u.role_id = r.id"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
}

func TestSelect_OrConditionInWhere(t *testing.T) {
	sql, args := Select("id", "email", "name").
		From("users").
		Where("deleted_at IS NULL").
		Where("(email LIKE ? OR name LIKE ?)", "%john%", "%john%").
		OrderBy("id", "DESC").
		Limit(10).
		Offset(0).
		Build()

	wantSQL := "SELECT id, email, name FROM users WHERE deleted_at IS NULL AND (email LIKE ? OR name LIKE ?) ORDER BY id DESC LIMIT ? OFFSET ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"%john%", "%john%", 10, 0}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_NoWhere(t *testing.T) {
	sql, args := Select("*").
		From("settings").
		OrderBy("key", "ASC").
		Build()

	wantSQL := "SELECT * FROM settings ORDER BY key ASC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestCount_Basic(t *testing.T) {
	sql, args := Count("users").
		Build()

	wantSQL := "SELECT COUNT(*) FROM users"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestCount_Where(t *testing.T) {
	sql, args := Count("users").
		Where("deleted_at IS NULL").
		Where("is_active = ?", 1).
		Build()

	wantSQL := "SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND is_active = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{1}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestCount_SearchPattern(t *testing.T) {
	sql, args := Count("users").
		Where("deleted_at IS NULL").
		Where("(email LIKE ? OR name LIKE ?)", "%test%", "%test%").
		Build()

	wantSQL := "SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND (email LIKE ? OR name LIKE ?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"%test%", "%test%"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestInsert_Basic(t *testing.T) {
	sql, args := Insert("users").
		Set("email", "john@example.com").
		Set("name", "John").
		Set("created_at", "2026-01-01T00:00:00Z").
		Build()

	wantSQL := "INSERT INTO users (email, name, created_at) VALUES (?, ?, ?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"john@example.com", "John", "2026-01-01T00:00:00Z"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestInsert_OrIgnore(t *testing.T) {
	sql, args := Insert("settings").
		OrIgnore().
		Set("key", "app.name").
		Set("value", "Stanza").
		Set("group_name", "general").
		Build()

	wantSQL := "INSERT OR IGNORE INTO settings (key, value, group_name) VALUES (?, ?, ?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"app.name", "Stanza", "general"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestInsert_ConditionalFields(t *testing.T) {
	password := "hashed_pw"

	b := Insert("users").
		Set("email", "john@example.com").
		Set("name", "John")

	if password != "" {
		b.Set("password", password)
	}

	b.Set("created_at", "2026-01-01T00:00:00Z")

	sql, args := b.Build()

	wantSQL := "INSERT INTO users (email, name, password, created_at) VALUES (?, ?, ?, ?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"john@example.com", "John", "hashed_pw", "2026-01-01T00:00:00Z"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_Basic(t *testing.T) {
	sql, args := Update("settings").
		Set("value", "new_value").
		Set("updated_at", "2026-01-01T00:00:00Z").
		Where("key = ?", "app.name").
		Build()

	wantSQL := "UPDATE settings SET value = ?, updated_at = ? WHERE key = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"new_value", "2026-01-01T00:00:00Z", "app.name"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_MultipleWhere(t *testing.T) {
	sql, args := Update("admins").
		Set("name", "Jane").
		Set("role", "editor").
		Set("updated_at", "2026-01-01T00:00:00Z").
		Where("id = ?", 42).
		Where("deleted_at IS NULL").
		Build()

	wantSQL := "UPDATE admins SET name = ?, role = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"Jane", "editor", "2026-01-01T00:00:00Z", 42}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_ConditionalPassword(t *testing.T) {
	password := "new_hash"

	b := Update("admins").
		Set("name", "Jane").
		Set("role", "admin").
		Set("is_active", 1)

	if password != "" {
		b.Set("password", password)
	}

	b.Set("updated_at", "2026-01-01T00:00:00Z")

	sql, args := b.Where("id = ?", 1).
		Where("deleted_at IS NULL").
		Build()

	wantSQL := "UPDATE admins SET name = ?, role = ?, is_active = ?, password = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"Jane", "admin", 1, "new_hash", "2026-01-01T00:00:00Z", 1}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_SoftDelete(t *testing.T) {
	sql, args := Update("users").
		Set("deleted_at", "2026-01-01T00:00:00Z").
		Set("is_active", 0).
		Set("updated_at", "2026-01-01T00:00:00Z").
		Where("id = ?", 5).
		Where("deleted_at IS NULL").
		Build()

	wantSQL := "UPDATE users SET deleted_at = ?, is_active = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2026-01-01T00:00:00Z", 0, "2026-01-01T00:00:00Z", 5}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestDelete_Basic(t *testing.T) {
	sql, args := Delete("refresh_tokens").
		Where("token_hash = ?", "abc123").
		Build()

	wantSQL := "DELETE FROM refresh_tokens WHERE token_hash = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"abc123"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestDelete_MultipleWhere(t *testing.T) {
	sql, args := Delete("refresh_tokens").
		Where("entity_type = ?", "admin").
		Where("entity_id = ?", "42").
		Build()

	wantSQL := "DELETE FROM refresh_tokens WHERE entity_type = ? AND entity_id = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"admin", "42"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestDelete_NoWhere(t *testing.T) {
	sql, args := Delete("temp_data").Build()

	wantSQL := "DELETE FROM temp_data"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestSelect_FullPaginationPattern(t *testing.T) {
	// This mirrors the exact pattern from adminusers/usermgmt modules:
	// 1. Count query for total
	// 2. Select query with limit/offset

	countSQL, countArgs := Count("admins").
		Where("deleted_at IS NULL").
		Build()

	wantCountSQL := "SELECT COUNT(*) FROM admins WHERE deleted_at IS NULL"
	if countSQL != wantCountSQL {
		t.Errorf("count sql = %q, want %q", countSQL, wantCountSQL)
	}
	if len(countArgs) != 0 {
		t.Errorf("count args = %v, want empty", countArgs)
	}

	listSQL, listArgs := Select("id", "email", "name", "role", "is_active", "created_at", "updated_at").
		From("admins").
		Where("deleted_at IS NULL").
		OrderBy("id", "ASC").
		Limit(20).
		Offset(0).
		Build()

	wantListSQL := "SELECT id, email, name, role, is_active, created_at, updated_at FROM admins WHERE deleted_at IS NULL ORDER BY id ASC LIMIT ? OFFSET ?"
	if listSQL != wantListSQL {
		t.Errorf("list sql = %q, want %q", listSQL, wantListSQL)
	}
	wantListArgs := []any{20, 0}
	if !reflect.DeepEqual(listArgs, wantListArgs) {
		t.Errorf("list args = %v, want %v", listArgs, wantListArgs)
	}
}

func TestSelect_SearchWithPagination(t *testing.T) {
	// Mirrors usermgmt search pattern
	search := "%john%"

	sql, args := Select("id", "email", "name", "is_active", "created_at", "updated_at").
		From("users").
		Where("deleted_at IS NULL").
		Where("(email LIKE ? OR name LIKE ?)", search, search).
		OrderBy("id", "DESC").
		Limit(10).
		Offset(0).
		Build()

	wantSQL := "SELECT id, email, name, is_active, created_at, updated_at FROM users WHERE deleted_at IS NULL AND (email LIKE ? OR name LIKE ?) ORDER BY id DESC LIMIT ? OFFSET ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"%john%", "%john%", 10, 0}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_SessionListWithJoin(t *testing.T) {
	// Mirrors adminsessions pattern
	sql, args := Select(
		"rt.id", "rt.entity_type", "rt.entity_id",
		"rt.created_at", "rt.expires_at",
		"COALESCE(a.email, '')", "COALESCE(a.name, '')",
	).
		From("refresh_tokens rt").
		LeftJoin("admins a", "rt.entity_type = 'admin' AND rt.entity_id = CAST(a.id AS TEXT)").
		Where("rt.expires_at > ?", "2026-03-21T00:00:00Z").
		OrderBy("rt.created_at", "DESC").
		Build()

	wantSQL := "SELECT rt.id, rt.entity_type, rt.entity_id, rt.created_at, rt.expires_at, COALESCE(a.email, ''), COALESCE(a.name, '') FROM refresh_tokens rt LEFT JOIN admins a ON rt.entity_type = 'admin' AND rt.entity_id = CAST(a.id AS TEXT) WHERE rt.expires_at > ? ORDER BY rt.created_at DESC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2026-03-21T00:00:00Z"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestInsert_SingleColumn(t *testing.T) {
	sql, args := Insert("flags").
		Set("name", "beta").
		Build()

	wantSQL := "INSERT INTO flags (name) VALUES (?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"beta"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_NoWhere(t *testing.T) {
	sql, args := Update("settings").
		Set("value", "updated").
		Build()

	wantSQL := "UPDATE settings SET value = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"updated"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_LimitOnly(t *testing.T) {
	sql, args := Select("id").
		From("users").
		Limit(5).
		Build()

	wantSQL := "SELECT id FROM users LIMIT ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{5}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestInsert_NilValue(t *testing.T) {
	sql, args := Insert("users").
		Set("email", "test@test.com").
		Set("name", nil).
		Build()

	wantSQL := "INSERT INTO users (email, name) VALUES (?, ?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"test@test.com", nil}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_OffsetOnly(t *testing.T) {
	sql, args := Select("id").
		From("users").
		Offset(10).
		Build()

	wantSQL := "SELECT id FROM users OFFSET ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{10}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_LimitZero(t *testing.T) {
	sql, args := Select("id").
		From("users").
		Limit(0).
		Build()

	wantSQL := "SELECT id FROM users LIMIT ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{0}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_MultipleJoins(t *testing.T) {
	sql, _ := Select("u.id", "r.name", "d.name").
		From("users u").
		Join("roles r", "u.role_id = r.id").
		LeftJoin("departments d", "u.dept_id = d.id").
		Build()

	wantSQL := "SELECT u.id, r.name, d.name FROM users u JOIN roles r ON u.role_id = r.id LEFT JOIN departments d ON u.dept_id = d.id"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
}

func TestSelect_SingleColumn(t *testing.T) {
	sql, args := Select("count(*)").
		From("users").
		Where("is_active = ?", 1).
		Build()

	wantSQL := "SELECT count(*) FROM users WHERE is_active = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{1}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestCount_NoWhere(t *testing.T) {
	sql, args := Count("settings").Build()

	wantSQL := "SELECT COUNT(*) FROM settings"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestDelete_SingleWhere(t *testing.T) {
	sql, args := Delete("sessions").
		Where("expires_at < ?", "2026-01-01T00:00:00Z").
		Build()

	wantSQL := "DELETE FROM sessions WHERE expires_at < ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2026-01-01T00:00:00Z"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_WhereWithMultipleArgs(t *testing.T) {
	sql, args := Select("id").
		From("jobs").
		Where("status IN (?, ?)", "pending", "running").
		Build()

	wantSQL := "SELECT id FROM jobs WHERE status IN (?, ?)"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"pending", "running"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_SingleSet(t *testing.T) {
	sql, args := Update("users").
		Set("is_active", 0).
		Where("id = ?", 1).
		Build()

	wantSQL := "UPDATE users SET is_active = ? WHERE id = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{0, 1}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_JoinWithWhere(t *testing.T) {
	sql, args := Select("u.id", "r.name").
		From("users u").
		Join("roles r", "u.role_id = r.id").
		Where("u.is_active = ?", 1).
		Where("r.level > ?", 5).
		OrderBy("u.id", "ASC").
		Limit(10).
		Build()

	wantSQL := "SELECT u.id, r.name FROM users u JOIN roles r ON u.role_id = r.id WHERE u.is_active = ? AND r.level > ? ORDER BY u.id ASC LIMIT ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{1, 5, 10}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_SetArgOrder(t *testing.T) {
	// Verify SET args come before WHERE args
	sql, args := Update("users").
		Set("name", "Alice").
		Set("email", "alice@test.com").
		Where("id = ?", 10).
		Where("org_id = ?", 3).
		Build()

	wantSQL := "UPDATE users SET name = ?, email = ? WHERE id = ? AND org_id = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"Alice", "alice@test.com", 10, 3}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

// ---------------------------------------------------------------------------
// SetExpr
// ---------------------------------------------------------------------------

func TestUpdate_SetExprIncrement(t *testing.T) {
	sql, args := Update("api_keys").
		Set("last_used_at", "2024-01-01").
		SetExpr("request_count", "request_count + 1").
		Where("id = ?", 42).
		Build()

	wantSQL := "UPDATE api_keys SET last_used_at = ?, request_count = request_count + 1 WHERE id = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2024-01-01", 42}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_SetExprWithArgs(t *testing.T) {
	sql, args := Update("counters").
		SetExpr("value", "value + ?", 5).
		Where("name = ?", "hits").
		Build()

	wantSQL := "UPDATE counters SET value = value + ? WHERE name = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{5, "hits"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_SetExprNoArgs(t *testing.T) {
	sql, args := Update("jobs").
		SetExpr("updated_at", "datetime('now')").
		Where("id = ?", 1).
		Build()

	wantSQL := "UPDATE jobs SET updated_at = datetime('now') WHERE id = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{1}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestUpdate_MixedSetAndSetExpr(t *testing.T) {
	sql, args := Update("api_keys").
		Set("last_used_at", "2024-01-01").
		SetExpr("request_count", "request_count + 1").
		Set("status", "active").
		Where("id = ?", 42).
		Build()

	wantSQL := "UPDATE api_keys SET last_used_at = ?, request_count = request_count + 1, status = ? WHERE id = ?"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	// Set args and SetExpr args interleaved in order, then WHERE args
	wantArgs := []any{"2024-01-01", "active", 42}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

// ---------------------------------------------------------------------------
// GroupBy / Having
// ---------------------------------------------------------------------------

func TestSelect_GroupBy(t *testing.T) {
	sql, args := Select("date(created_at) as day", "COUNT(*) as cnt").
		From("users").
		Where("created_at >= ?", "2024-01-01").
		GroupBy("day").
		OrderBy("day", "ASC").
		Build()

	wantSQL := "SELECT date(created_at) as day, COUNT(*) as cnt FROM users WHERE created_at >= ? GROUP BY day ORDER BY day ASC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2024-01-01"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_GroupByMultipleColumns(t *testing.T) {
	sql, args := Select("status", "type", "COUNT(*) as cnt").
		From("jobs").
		GroupBy("status", "type").
		Build()

	wantSQL := "SELECT status, type, COUNT(*) as cnt FROM jobs GROUP BY status, type"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestSelect_GroupByHaving(t *testing.T) {
	sql, args := Select("status", "COUNT(*) as cnt").
		From("jobs").
		GroupBy("status").
		Having("COUNT(*) > ?", 10).
		OrderBy("cnt", "DESC").
		Build()

	wantSQL := "SELECT status, COUNT(*) as cnt FROM jobs GROUP BY status HAVING COUNT(*) > ? ORDER BY cnt DESC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{10}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_GroupByHavingMultiple(t *testing.T) {
	sql, args := Select("type", "COUNT(*) as cnt", "AVG(duration) as avg_dur").
		From("jobs").
		Where("created_at >= ?", "2024-01-01").
		GroupBy("type").
		Having("COUNT(*) > ?", 5).
		Having("AVG(duration) < ?", 1000).
		OrderBy("cnt", "DESC").
		Build()

	wantSQL := "SELECT type, COUNT(*) as cnt, AVG(duration) as avg_dur FROM jobs WHERE created_at >= ? GROUP BY type HAVING COUNT(*) > ? AND AVG(duration) < ? ORDER BY cnt DESC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2024-01-01", 5, 1000}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_GroupByWithCaseExpr(t *testing.T) {
	// Dashboard-style aggregation: jobs by day with conditional sums
	sql, args := Select(
		"date(created_at) as day",
		"SUM(CASE WHEN status IN ('completed') THEN 1 ELSE 0 END) as completed",
		"SUM(CASE WHEN status IN ('failed','dead') THEN 1 ELSE 0 END) as failed",
	).
		From("_queue_jobs").
		Where("created_at >= ?", "2024-01-01").
		GroupBy("day").
		OrderBy("day", "ASC").
		Build()

	wantSQL := "SELECT date(created_at) as day, SUM(CASE WHEN status IN ('completed') THEN 1 ELSE 0 END) as completed, SUM(CASE WHEN status IN ('failed','dead') THEN 1 ELSE 0 END) as failed FROM _queue_jobs WHERE created_at >= ? GROUP BY day ORDER BY day ASC"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	wantArgs := []any{"2024-01-01"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestSelect_GroupByNoWhere(t *testing.T) {
	sql, args := Select("status", "COUNT(*)").
		From("jobs").
		GroupBy("status").
		Build()

	wantSQL := "SELECT status, COUNT(*) FROM jobs GROUP BY status"
	if sql != wantSQL {
		t.Errorf("sql = %q, want %q", sql, wantSQL)
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}
