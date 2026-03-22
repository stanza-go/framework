package sqlite

import (
	"strings"
)

// EscapeLike escapes the special characters %, _, and \ in s so that it
// can be used as a literal pattern in a LIKE clause with ESCAPE '\'.
// The caller is responsible for wrapping the result with % for prefix,
// suffix, or contains matching:
//
//	like := "%" + sqlite.EscapeLike(search) + "%"
//	q.Where("name LIKE ? ESCAPE '\\'", like)
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ---------------------------------------------------------------------------
// Aggregate column helpers
// ---------------------------------------------------------------------------

// Sum returns a SUM(column) expression for use in Select columns.
//
//	sqlite.Select(sqlite.Sum("amount")).From("orders").Build()
//	// → SELECT SUM(amount) FROM orders
func Sum(column string) string { return "SUM(" + column + ")" }

// Avg returns an AVG(column) expression for use in Select columns.
//
//	sqlite.Select(sqlite.Avg("duration")).From("jobs").Build()
//	// → SELECT AVG(duration) FROM jobs
func Avg(column string) string { return "AVG(" + column + ")" }

// Min returns a MIN(column) expression for use in Select columns.
//
//	sqlite.Select(sqlite.Min("created_at")).From("users").Build()
//	// → SELECT MIN(created_at) FROM users
func Min(column string) string { return "MIN(" + column + ")" }

// Max returns a MAX(column) expression for use in Select columns.
//
//	sqlite.Select(sqlite.Max("price")).From("products").Build()
//	// → SELECT MAX(price) FROM products
func Max(column string) string { return "MAX(" + column + ")" }

// As appends an AS alias to a column expression. Useful with aggregate helpers.
//
//	sqlite.Select("status", sqlite.As(sqlite.Sum("amount"), "total")).
//		From("orders").GroupBy("status").Build()
//	// → SELECT status, SUM(amount) AS total FROM orders GROUP BY status
func As(expr, alias string) string { return expr + " AS " + alias }

// Coalesce returns a COALESCE(column, fallback) expression. The fallback
// is a raw SQL literal — use "''" for an empty string, "0" for zero, etc.
//
//	sqlite.Select("id", sqlite.Coalesce("deleted_at", "''")).From("users").Build()
//	// → SELECT id, COALESCE(deleted_at, '') FROM users
func Coalesce(column, fallback string) string {
	return "COALESCE(" + column + ", " + fallback + ")"
}

// CoalesceEmpty returns a COALESCE(column, '') expression that converts
// NULL to an empty string. This is the most common COALESCE pattern when
// scanning nullable TEXT columns into Go strings.
//
//	sqlite.Select("id", sqlite.CoalesceEmpty("read_at"), "created_at").
//		From("notifications").Build()
//	// → SELECT id, COALESCE(read_at, ''), created_at FROM notifications
func CoalesceEmpty(column string) string {
	return Coalesce(column, "''")
}

// whereClause holds a condition fragment and its bound arguments.
type whereClause struct {
	cond string
	args []any
}

// Condition represents a single SQL condition with bound arguments.
// Create with Cond for use with WhereOr.
type Condition struct {
	cond string
	args []any
}

// Cond creates a condition for use with WhereOr.
//
//	sqlite.Cond("status = ?", "active")
//	sqlite.Cond("deleted_at IS NULL")
func Cond(cond string, args ...any) Condition {
	return Condition{cond: cond, args: args}
}

// whereOrClause builds a parenthesized OR group from multiple conditions.
// Returns false if fewer than 2 conditions are provided.
func whereOrClause(conds []Condition) (whereClause, bool) {
	if len(conds) < 2 {
		return whereClause{}, false
	}
	var sb strings.Builder
	var args []any
	sb.WriteByte('(')
	for i, c := range conds {
		if i > 0 {
			sb.WriteString(" OR ")
		}
		sb.WriteString(c.cond)
		args = append(args, c.args...)
	}
	sb.WriteByte(')')
	return whereClause{cond: sb.String(), args: args}, true
}

// orderByClause holds a column expression and sort direction.
type orderByClause struct {
	expr string
	dir  string
}

// joinClause holds a JOIN type, table, and ON condition.
type joinClause struct {
	kind  string
	table string
	on    string
}

// inPlaceholders returns a parenthesized comma-separated placeholder string.
// inPlaceholders(3) returns "(?, ?, ?)".
func inPlaceholders(n int) string {
	var sb strings.Builder
	sb.WriteByte('(')
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('?')
	}
	sb.WriteByte(')')
	return sb.String()
}

// whereSearchClause builds a parenthesized multi-column LIKE condition for
// contains-search. Returns false if search is empty or no columns provided.
func whereSearchClause(search string, columns []string) (whereClause, bool) {
	if search == "" || len(columns) == 0 {
		return whereClause{}, false
	}
	like := "%" + EscapeLike(search) + "%"
	var sb strings.Builder
	args := make([]any, 0, len(columns))
	sb.WriteByte('(')
	for i, col := range columns {
		if i > 0 {
			sb.WriteString(" OR ")
		}
		sb.WriteString(col)
		sb.WriteString(" LIKE ? ESCAPE '\\'")
		args = append(args, like)
	}
	sb.WriteByte(')')
	return whereClause{cond: sb.String(), args: args}, true
}

// subSelectClause builds a "column OP (SELECT ...)" condition by rendering
// the given SelectBuilder as a subquery.
func subSelectClause(column, op string, sub *SelectBuilder) whereClause {
	subSQL, subArgs := sub.Build()
	return whereClause{
		cond: column + " " + op + " (" + subSQL + ")",
		args: subArgs,
	}
}

// existsClause builds an "EXISTS (SELECT ...)" or "NOT EXISTS (SELECT ...)"
// condition by rendering the given SelectBuilder as a subquery.
func existsClause(not bool, sub *SelectBuilder) whereClause {
	subSQL, subArgs := sub.Build()
	prefix := "EXISTS"
	if not {
		prefix = "NOT EXISTS"
	}
	return whereClause{
		cond: prefix + " (" + subSQL + ")",
		args: subArgs,
	}
}

// appendWheres writes WHERE clauses to the builder and collects arguments.
func appendWheres(sb *strings.Builder, wheres []whereClause, args []any) []any {
	if len(wheres) == 0 {
		return args
	}
	sb.WriteString(" WHERE ")
	for i, w := range wheres {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(w.cond)
		args = append(args, w.args...)
	}
	return args
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// SelectBuilder builds SELECT queries.
type SelectBuilder struct {
	columns   []string
	table     string
	distinct  bool
	joins     []joinClause
	wheres    []whereClause
	groupBys  []string
	havings   []whereClause
	orderBys  []orderByClause
	limit     int
	offset    int
	hasLimit  bool
	hasOffset bool
}

// Select starts building a SELECT query with the given columns.
func Select(columns ...string) *SelectBuilder {
	return &SelectBuilder{columns: columns}
}

// From sets the table to select from.
func (b *SelectBuilder) From(table string) *SelectBuilder {
	b.table = table
	return b
}

// Distinct adds the DISTINCT keyword to the SELECT query, eliminating
// duplicate rows from the result set.
//
//	sqlite.Select("role").From("users").Distinct().Build()
//	// → SELECT DISTINCT role FROM users
func (b *SelectBuilder) Distinct() *SelectBuilder {
	b.distinct = true
	return b
}

// Where adds an AND condition. Multiple calls are joined with AND.
// Use parenthesized expressions for OR: Where("(a = ? OR b = ?)", x, y).
func (b *SelectBuilder) Where(cond string, args ...any) *SelectBuilder {
	b.wheres = append(b.wheres, whereClause{cond: cond, args: args})
	return b
}

// WhereNull adds a "column IS NULL" condition.
func (b *SelectBuilder) WhereNull(column string) *SelectBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NULL"})
	return b
}

// WhereNotNull adds a "column IS NOT NULL" condition.
func (b *SelectBuilder) WhereNotNull(column string) *SelectBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NOT NULL"})
	return b
}

// WhereIn adds a "column IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 0" (always false).
func (b *SelectBuilder) WhereIn(column string, values ...any) *SelectBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 0"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereNotIn adds a "column NOT IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 1" (always true — everything is NOT IN an empty set).
func (b *SelectBuilder) WhereNotIn(column string, values ...any) *SelectBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 1"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " NOT IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereOr adds conditions grouped with OR. Each Cond is OR'd together and
// the group is parenthesized so it composes correctly with other AND conditions.
// Requires at least 2 conditions; fewer is a no-op (use Where for single conditions).
//
//	Delete("api_keys").
//		WhereOr(
//			sqlite.Cond("revoked_at IS NOT NULL AND revoked_at < ?", cutoff),
//			sqlite.Cond("expires_at IS NOT NULL AND expires_at < ?", cutoff),
//		)
//	// → WHERE (revoked_at IS NOT NULL AND revoked_at < ? OR expires_at IS NOT NULL AND expires_at < ?)
func (b *SelectBuilder) WhereOr(conds ...Condition) *SelectBuilder {
	if wc, ok := whereOrClause(conds); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereSearch adds a multi-column contains-search condition. The search term
// is escaped with EscapeLike and wrapped in % for contains matching. Multiple
// columns are OR'd together and the whole condition is parenthesized so it
// composes correctly with other AND conditions. If search is empty or no
// columns are provided, this is a no-op.
//
//	q.Where("deleted_at IS NULL").WhereSearch("john", "email", "name")
//	// → WHERE deleted_at IS NULL AND (email LIKE '%john%' ESCAPE '\' OR name LIKE '%john%' ESCAPE '\')
func (b *SelectBuilder) WhereSearch(search string, columns ...string) *SelectBuilder {
	if wc, ok := whereSearchClause(search, columns); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereInSelect adds a "column IN (SELECT ...)" condition using a subquery.
// The subquery is built from the given SelectBuilder, and its arguments are
// merged into the outer query's argument list.
//
//	Select("*").From("users").
//		WhereInSelect("id", Select("user_id").From("active_sessions").Where("expires_at > ?", now))
//	// → WHERE id IN (SELECT user_id FROM active_sessions WHERE expires_at > ?)
func (b *SelectBuilder) WhereInSelect(column string, sub *SelectBuilder) *SelectBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "IN", sub))
	return b
}

// WhereNotInSelect adds a "column NOT IN (SELECT ...)" condition using a subquery.
//
//	Delete("notifications").
//		WhereNotInSelect("entity_id", Select("id").From("admins").Where("is_active = 1"))
//	// → WHERE entity_id NOT IN (SELECT id FROM admins WHERE is_active = 1)
func (b *SelectBuilder) WhereNotInSelect(column string, sub *SelectBuilder) *SelectBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "NOT IN", sub))
	return b
}

// WhereExists adds an "EXISTS (SELECT ...)" condition using a subquery.
// The subquery typically uses SELECT 1 and correlates with the outer query.
//
//	Select("*").From("users u").
//		WhereExists(Select("1").From("orders o").Where("o.user_id = u.id"))
//	// → WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id)
func (b *SelectBuilder) WhereExists(sub *SelectBuilder) *SelectBuilder {
	b.wheres = append(b.wheres, existsClause(false, sub))
	return b
}

// WhereNotExists adds a "NOT EXISTS (SELECT ...)" condition using a subquery.
//
//	Select("*").From("users u").
//		WhereNotExists(Select("1").From("orders o").Where("o.user_id = u.id"))
//	// → WHERE NOT EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id)
func (b *SelectBuilder) WhereNotExists(sub *SelectBuilder) *SelectBuilder {
	b.wheres = append(b.wheres, existsClause(true, sub))
	return b
}

// Join adds an INNER JOIN clause.
func (b *SelectBuilder) Join(table, on string) *SelectBuilder {
	b.joins = append(b.joins, joinClause{kind: "JOIN", table: table, on: on})
	return b
}

// LeftJoin adds a LEFT JOIN clause.
func (b *SelectBuilder) LeftJoin(table, on string) *SelectBuilder {
	b.joins = append(b.joins, joinClause{kind: "LEFT JOIN", table: table, on: on})
	return b
}

// GroupBy adds GROUP BY columns. Multiple calls append to the list.
func (b *SelectBuilder) GroupBy(columns ...string) *SelectBuilder {
	b.groupBys = append(b.groupBys, columns...)
	return b
}

// Having adds a HAVING condition (used with GROUP BY). Multiple calls
// are joined with AND, same as Where.
func (b *SelectBuilder) Having(cond string, args ...any) *SelectBuilder {
	b.havings = append(b.havings, whereClause{cond: cond, args: args})
	return b
}

// OrderBy adds an ORDER BY clause. dir should be "ASC" or "DESC".
func (b *SelectBuilder) OrderBy(expr, dir string) *SelectBuilder {
	b.orderBys = append(b.orderBys, orderByClause{expr: expr, dir: dir})
	return b
}

// Limit sets the LIMIT clause. The value is bound as a parameter.
func (b *SelectBuilder) Limit(n int) *SelectBuilder {
	b.limit = n
	b.hasLimit = true
	return b
}

// Offset sets the OFFSET clause. The value is bound as a parameter.
func (b *SelectBuilder) Offset(n int) *SelectBuilder {
	b.offset = n
	b.hasOffset = true
	return b
}

// Build produces the SQL string and argument slice.
func (b *SelectBuilder) Build() (string, []any) {
	var sb strings.Builder
	var args []any

	if b.distinct {
		sb.WriteString("SELECT DISTINCT ")
	} else {
		sb.WriteString("SELECT ")
	}
	sb.WriteString(strings.Join(b.columns, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(b.table)

	for _, j := range b.joins {
		sb.WriteByte(' ')
		sb.WriteString(j.kind)
		sb.WriteByte(' ')
		sb.WriteString(j.table)
		sb.WriteString(" ON ")
		sb.WriteString(j.on)
	}

	args = appendWheres(&sb, b.wheres, args)

	if len(b.groupBys) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(b.groupBys, ", "))
	}

	if len(b.havings) > 0 {
		sb.WriteString(" HAVING ")
		for i, h := range b.havings {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(h.cond)
			args = append(args, h.args...)
		}
	}

	if len(b.orderBys) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, o := range b.orderBys {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(o.expr)
			sb.WriteByte(' ')
			sb.WriteString(o.dir)
		}
	}

	if b.hasLimit {
		sb.WriteString(" LIMIT ?")
		args = append(args, b.limit)
	}
	if b.hasOffset {
		sb.WriteString(" OFFSET ?")
		args = append(args, b.offset)
	}

	return sb.String(), args
}

// ---------------------------------------------------------------------------
// COUNT
// ---------------------------------------------------------------------------

// CountBuilder builds SELECT COUNT(*) queries.
type CountBuilder struct {
	table  string
	wheres []whereClause
}

// Count starts building a SELECT COUNT(*) query.
func Count(table string) *CountBuilder {
	return &CountBuilder{table: table}
}

// CountFrom creates a COUNT query reusing the table and WHERE clauses from a
// SelectBuilder. This eliminates the need to duplicate WHERE conditions when
// building both a SELECT and a COUNT for paginated queries. JOINs, ORDER BY,
// LIMIT, and OFFSET are excluded — for LEFT JOINs this is correct because
// they preserve all rows from the left table.
func CountFrom(sb *SelectBuilder) *CountBuilder {
	cb := &CountBuilder{table: sb.table}
	if len(sb.wheres) > 0 {
		cb.wheres = make([]whereClause, len(sb.wheres))
		copy(cb.wheres, sb.wheres)
	}
	return cb
}

// Where adds an AND condition.
func (b *CountBuilder) Where(cond string, args ...any) *CountBuilder {
	b.wheres = append(b.wheres, whereClause{cond: cond, args: args})
	return b
}

// WhereNull adds a "column IS NULL" condition.
func (b *CountBuilder) WhereNull(column string) *CountBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NULL"})
	return b
}

// WhereNotNull adds a "column IS NOT NULL" condition.
func (b *CountBuilder) WhereNotNull(column string) *CountBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NOT NULL"})
	return b
}

// WhereIn adds a "column IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 0" (always false).
func (b *CountBuilder) WhereIn(column string, values ...any) *CountBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 0"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereNotIn adds a "column NOT IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 1" (always true).
func (b *CountBuilder) WhereNotIn(column string, values ...any) *CountBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 1"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " NOT IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereOr adds conditions grouped with OR.
// See SelectBuilder.WhereOr for details.
func (b *CountBuilder) WhereOr(conds ...Condition) *CountBuilder {
	if wc, ok := whereOrClause(conds); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereSearch adds a multi-column contains-search condition.
// See SelectBuilder.WhereSearch for details.
func (b *CountBuilder) WhereSearch(search string, columns ...string) *CountBuilder {
	if wc, ok := whereSearchClause(search, columns); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereInSelect adds a "column IN (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereInSelect for details.
func (b *CountBuilder) WhereInSelect(column string, sub *SelectBuilder) *CountBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "IN", sub))
	return b
}

// WhereNotInSelect adds a "column NOT IN (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereNotInSelect for details.
func (b *CountBuilder) WhereNotInSelect(column string, sub *SelectBuilder) *CountBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "NOT IN", sub))
	return b
}

// WhereExists adds an "EXISTS (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereExists for details.
func (b *CountBuilder) WhereExists(sub *SelectBuilder) *CountBuilder {
	b.wheres = append(b.wheres, existsClause(false, sub))
	return b
}

// WhereNotExists adds a "NOT EXISTS (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereNotExists for details.
func (b *CountBuilder) WhereNotExists(sub *SelectBuilder) *CountBuilder {
	b.wheres = append(b.wheres, existsClause(true, sub))
	return b
}

// Build produces the SQL string and argument slice.
func (b *CountBuilder) Build() (string, []any) {
	var sb strings.Builder
	var args []any

	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(b.table)

	args = appendWheres(&sb, b.wheres, args)

	return sb.String(), args
}

// ---------------------------------------------------------------------------
// INSERT
// ---------------------------------------------------------------------------

// InsertBuilder builds INSERT queries. Use Set to add column-value pairs.
// This allows conditional column inclusion by chaining Set calls.
type InsertBuilder struct {
	table           string
	columns         []string
	values          []any
	orIgnore        bool
	conflictColumns []string
	updateColumns   []string
}

// Insert starts building an INSERT query for the given table.
func Insert(table string) *InsertBuilder {
	return &InsertBuilder{table: table}
}

// Set adds a column-value pair to the INSERT.
func (b *InsertBuilder) Set(column string, value any) *InsertBuilder {
	b.columns = append(b.columns, column)
	b.values = append(b.values, value)
	return b
}

// OrIgnore makes the statement INSERT OR IGNORE (skips on conflict).
func (b *InsertBuilder) OrIgnore() *InsertBuilder {
	b.orIgnore = true
	return b
}

// OnConflict adds an ON CONFLICT ... DO UPDATE SET clause for upsert
// behavior. conflictColumns are the unique columns that trigger the
// conflict (e.g., "user_id", "key"). updateColumns are the columns to
// update from the excluded row when a conflict occurs. The generated SQL
// uses "excluded.<col>" to reference the values from the attempted insert:
//
//	sqlite.Insert("user_settings").
//		Set("user_id", uid).
//		Set("key", k).
//		Set("value", v).
//		Set("updated_at", now).
//		OnConflict([]string{"user_id", "key"}, []string{"value", "updated_at"})
//
// Produces:
//
//	INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, ?)
//	ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
func (b *InsertBuilder) OnConflict(conflictColumns, updateColumns []string) *InsertBuilder {
	b.conflictColumns = conflictColumns
	b.updateColumns = updateColumns
	return b
}

// Build produces the SQL string and argument slice.
func (b *InsertBuilder) Build() (string, []any) {
	var sb strings.Builder

	if b.orIgnore {
		sb.WriteString("INSERT OR IGNORE INTO ")
	} else {
		sb.WriteString("INSERT INTO ")
	}
	sb.WriteString(b.table)
	sb.WriteString(" (")
	sb.WriteString(strings.Join(b.columns, ", "))
	sb.WriteString(") VALUES (")
	for i := range b.columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('?')
	}
	sb.WriteByte(')')

	if len(b.conflictColumns) > 0 && len(b.updateColumns) > 0 {
		sb.WriteString(" ON CONFLICT(")
		sb.WriteString(strings.Join(b.conflictColumns, ", "))
		sb.WriteString(") DO UPDATE SET ")
		for i, col := range b.updateColumns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(col)
			sb.WriteString(" = excluded.")
			sb.WriteString(col)
		}
	}

	return sb.String(), b.values
}

// ---------------------------------------------------------------------------
// INSERT BATCH
// ---------------------------------------------------------------------------

// InsertBatchBuilder builds multi-row INSERT queries. Use Columns to define
// the column list, then Row to add each row of values. This generates a
// single INSERT with multiple VALUES tuples, which is more efficient than
// executing separate INSERT statements in a loop.
//
//	sqlite.InsertBatch("settings").
//		Columns("key", "value", "group_name").
//		Row("site_name", "Stanza", "general").
//		Row("site_url", "https://stanza.dev", "general").
//		OrIgnore().
//		Build()
//
// Produces:
//
//	INSERT OR IGNORE INTO settings (key, value, group_name) VALUES (?, ?, ?), (?, ?, ?)
type InsertBatchBuilder struct {
	table           string
	columns         []string
	rows            [][]any
	orIgnore        bool
	conflictColumns []string
	updateColumns   []string
}

// InsertBatch starts building a multi-row INSERT query for the given table.
func InsertBatch(table string) *InsertBatchBuilder {
	return &InsertBatchBuilder{table: table}
}

// Columns sets the column list for the INSERT. Must be called before Row.
func (b *InsertBatchBuilder) Columns(columns ...string) *InsertBatchBuilder {
	b.columns = columns
	return b
}

// Row adds a row of values. The number of values must match the number of
// columns set by Columns. Multiple calls add multiple rows.
func (b *InsertBatchBuilder) Row(values ...any) *InsertBatchBuilder {
	b.rows = append(b.rows, values)
	return b
}

// OrIgnore makes the statement INSERT OR IGNORE (skips on conflict).
func (b *InsertBatchBuilder) OrIgnore() *InsertBatchBuilder {
	b.orIgnore = true
	return b
}

// OnConflict adds an ON CONFLICT ... DO UPDATE SET clause for batch upsert.
// See InsertBuilder.OnConflict for details.
func (b *InsertBatchBuilder) OnConflict(conflictColumns, updateColumns []string) *InsertBatchBuilder {
	b.conflictColumns = conflictColumns
	b.updateColumns = updateColumns
	return b
}

// Build produces the SQL string and argument slice. All row values are
// flattened into a single argument slice in row order.
func (b *InsertBatchBuilder) Build() (string, []any) {
	var sb strings.Builder
	args := make([]any, 0, len(b.columns)*len(b.rows))

	if b.orIgnore {
		sb.WriteString("INSERT OR IGNORE INTO ")
	} else {
		sb.WriteString("INSERT INTO ")
	}
	sb.WriteString(b.table)
	sb.WriteString(" (")
	sb.WriteString(strings.Join(b.columns, ", "))
	sb.WriteString(") VALUES ")

	placeholder := inPlaceholders(len(b.columns))
	for i, row := range b.rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(placeholder)
		args = append(args, row...)
	}

	if len(b.conflictColumns) > 0 && len(b.updateColumns) > 0 {
		sb.WriteString(" ON CONFLICT(")
		sb.WriteString(strings.Join(b.conflictColumns, ", "))
		sb.WriteString(") DO UPDATE SET ")
		for i, col := range b.updateColumns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(col)
			sb.WriteString(" = excluded.")
			sb.WriteString(col)
		}
	}

	return sb.String(), args
}

// ---------------------------------------------------------------------------
// UPDATE
// ---------------------------------------------------------------------------

// setClause holds a SET assignment — either "col = ?" or "col = expr".
type setClause struct {
	sql  string
	args []any
}

// UpdateBuilder builds UPDATE queries. Use Set to add column-value pairs,
// SetExpr for raw SQL expressions, and Where for conditions.
type UpdateBuilder struct {
	table  string
	sets   []setClause
	wheres []whereClause
}

// Update starts building an UPDATE query for the given table.
func Update(table string) *UpdateBuilder {
	return &UpdateBuilder{table: table}
}

// Set adds a column = ? assignment.
func (b *UpdateBuilder) Set(column string, value any) *UpdateBuilder {
	b.sets = append(b.sets, setClause{sql: column + " = ?", args: []any{value}})
	return b
}

// SetExpr adds a column = <expr> assignment using a raw SQL expression.
// Use this for computed updates like "request_count = request_count + 1"
// or "updated_at = datetime('now')". Pass args for any ? placeholders
// in the expression.
func (b *UpdateBuilder) SetExpr(column, expr string, args ...any) *UpdateBuilder {
	b.sets = append(b.sets, setClause{sql: column + " = " + expr, args: args})
	return b
}

// Where adds an AND condition.
func (b *UpdateBuilder) Where(cond string, args ...any) *UpdateBuilder {
	b.wheres = append(b.wheres, whereClause{cond: cond, args: args})
	return b
}

// WhereNull adds a "column IS NULL" condition.
func (b *UpdateBuilder) WhereNull(column string) *UpdateBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NULL"})
	return b
}

// WhereNotNull adds a "column IS NOT NULL" condition.
func (b *UpdateBuilder) WhereNotNull(column string) *UpdateBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NOT NULL"})
	return b
}

// WhereIn adds a "column IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 0" (always false).
func (b *UpdateBuilder) WhereIn(column string, values ...any) *UpdateBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 0"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereNotIn adds a "column NOT IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 1" (always true).
func (b *UpdateBuilder) WhereNotIn(column string, values ...any) *UpdateBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 1"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " NOT IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereOr adds conditions grouped with OR.
// See SelectBuilder.WhereOr for details.
func (b *UpdateBuilder) WhereOr(conds ...Condition) *UpdateBuilder {
	if wc, ok := whereOrClause(conds); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereSearch adds a multi-column contains-search condition.
// See SelectBuilder.WhereSearch for details.
func (b *UpdateBuilder) WhereSearch(search string, columns ...string) *UpdateBuilder {
	if wc, ok := whereSearchClause(search, columns); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereInSelect adds a "column IN (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereInSelect for details.
func (b *UpdateBuilder) WhereInSelect(column string, sub *SelectBuilder) *UpdateBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "IN", sub))
	return b
}

// WhereNotInSelect adds a "column NOT IN (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereNotInSelect for details.
func (b *UpdateBuilder) WhereNotInSelect(column string, sub *SelectBuilder) *UpdateBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "NOT IN", sub))
	return b
}

// WhereExists adds an "EXISTS (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereExists for details.
func (b *UpdateBuilder) WhereExists(sub *SelectBuilder) *UpdateBuilder {
	b.wheres = append(b.wheres, existsClause(false, sub))
	return b
}

// WhereNotExists adds a "NOT EXISTS (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereNotExists for details.
func (b *UpdateBuilder) WhereNotExists(sub *SelectBuilder) *UpdateBuilder {
	b.wheres = append(b.wheres, existsClause(true, sub))
	return b
}

// Build produces the SQL string and argument slice.
// SET arguments come before WHERE arguments in the returned slice.
func (b *UpdateBuilder) Build() (string, []any) {
	var sb strings.Builder
	var args []any

	sb.WriteString("UPDATE ")
	sb.WriteString(b.table)
	sb.WriteString(" SET ")
	for i, s := range b.sets {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(s.sql)
		args = append(args, s.args...)
	}

	args = appendWheres(&sb, b.wheres, args)

	return sb.String(), args
}

// ---------------------------------------------------------------------------
// DELETE
// ---------------------------------------------------------------------------

// DeleteBuilder builds DELETE queries.
type DeleteBuilder struct {
	table  string
	wheres []whereClause
}

// Delete starts building a DELETE query for the given table.
func Delete(table string) *DeleteBuilder {
	return &DeleteBuilder{table: table}
}

// Where adds an AND condition.
func (b *DeleteBuilder) Where(cond string, args ...any) *DeleteBuilder {
	b.wheres = append(b.wheres, whereClause{cond: cond, args: args})
	return b
}

// WhereNull adds a "column IS NULL" condition.
func (b *DeleteBuilder) WhereNull(column string) *DeleteBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NULL"})
	return b
}

// WhereNotNull adds a "column IS NOT NULL" condition.
func (b *DeleteBuilder) WhereNotNull(column string) *DeleteBuilder {
	b.wheres = append(b.wheres, whereClause{cond: column + " IS NOT NULL"})
	return b
}

// WhereIn adds a "column IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 0" (always false).
func (b *DeleteBuilder) WhereIn(column string, values ...any) *DeleteBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 0"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereNotIn adds a "column NOT IN (?, ?, ...)" condition. If values is empty,
// the condition becomes "1 = 1" (always true).
func (b *DeleteBuilder) WhereNotIn(column string, values ...any) *DeleteBuilder {
	if len(values) == 0 {
		b.wheres = append(b.wheres, whereClause{cond: "1 = 1"})
		return b
	}
	b.wheres = append(b.wheres, whereClause{
		cond: column + " NOT IN " + inPlaceholders(len(values)),
		args: values,
	})
	return b
}

// WhereOr adds conditions grouped with OR.
// See SelectBuilder.WhereOr for details.
func (b *DeleteBuilder) WhereOr(conds ...Condition) *DeleteBuilder {
	if wc, ok := whereOrClause(conds); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereSearch adds a multi-column contains-search condition.
// See SelectBuilder.WhereSearch for details.
func (b *DeleteBuilder) WhereSearch(search string, columns ...string) *DeleteBuilder {
	if wc, ok := whereSearchClause(search, columns); ok {
		b.wheres = append(b.wheres, wc)
	}
	return b
}

// WhereInSelect adds a "column IN (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereInSelect for details.
func (b *DeleteBuilder) WhereInSelect(column string, sub *SelectBuilder) *DeleteBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "IN", sub))
	return b
}

// WhereNotInSelect adds a "column NOT IN (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereNotInSelect for details.
func (b *DeleteBuilder) WhereNotInSelect(column string, sub *SelectBuilder) *DeleteBuilder {
	b.wheres = append(b.wheres, subSelectClause(column, "NOT IN", sub))
	return b
}

// WhereExists adds an "EXISTS (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereExists for details.
func (b *DeleteBuilder) WhereExists(sub *SelectBuilder) *DeleteBuilder {
	b.wheres = append(b.wheres, existsClause(false, sub))
	return b
}

// WhereNotExists adds a "NOT EXISTS (SELECT ...)" condition using a subquery.
// See SelectBuilder.WhereNotExists for details.
func (b *DeleteBuilder) WhereNotExists(sub *SelectBuilder) *DeleteBuilder {
	b.wheres = append(b.wheres, existsClause(true, sub))
	return b
}

// Build produces the SQL string and argument slice.
func (b *DeleteBuilder) Build() (string, []any) {
	var sb strings.Builder
	var args []any

	sb.WriteString("DELETE FROM ")
	sb.WriteString(b.table)

	args = appendWheres(&sb, b.wheres, args)

	return sb.String(), args
}
