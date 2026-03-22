package sqlite

import "strings"

// whereClause holds a condition fragment and its bound arguments.
type whereClause struct {
	cond string
	args []any
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
	joins     []joinClause
	wheres    []whereClause
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

// Where adds an AND condition. Multiple calls are joined with AND.
// Use parenthesized expressions for OR: Where("(a = ? OR b = ?)", x, y).
func (b *SelectBuilder) Where(cond string, args ...any) *SelectBuilder {
	b.wheres = append(b.wheres, whereClause{cond: cond, args: args})
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

	sb.WriteString("SELECT ")
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
	table    string
	columns  []string
	values   []any
	orIgnore bool
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

	return sb.String(), b.values
}

// ---------------------------------------------------------------------------
// UPDATE
// ---------------------------------------------------------------------------

// UpdateBuilder builds UPDATE queries. Use Set to add column-value pairs
// and Where for conditions. This allows conditional field updates by
// chaining Set calls only when needed.
type UpdateBuilder struct {
	table   string
	columns []string
	values  []any
	wheres  []whereClause
}

// Update starts building an UPDATE query for the given table.
func Update(table string) *UpdateBuilder {
	return &UpdateBuilder{table: table}
}

// Set adds a column = ? assignment.
func (b *UpdateBuilder) Set(column string, value any) *UpdateBuilder {
	b.columns = append(b.columns, column)
	b.values = append(b.values, value)
	return b
}

// Where adds an AND condition.
func (b *UpdateBuilder) Where(cond string, args ...any) *UpdateBuilder {
	b.wheres = append(b.wheres, whereClause{cond: cond, args: args})
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
	for i, col := range b.columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(col)
		sb.WriteString(" = ?")
	}
	args = append(args, b.values...)

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

// Build produces the SQL string and argument slice.
func (b *DeleteBuilder) Build() (string, []any) {
	var sb strings.Builder
	var args []any

	sb.WriteString("DELETE FROM ")
	sb.WriteString(b.table)

	args = appendWheres(&sb, b.wheres, args)

	return sb.String(), args
}
