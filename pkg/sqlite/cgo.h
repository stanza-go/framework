#ifndef STANZA_CGO_H
#define STANZA_CGO_H

#include <stdlib.h>
#include "sqlite3.h"

static inline int _busy_timeout(sqlite3 *db, int ms) {
	return sqlite3_busy_timeout(db, ms);
}

static inline int _open(const char *filename, sqlite3 **db, int flags) {
	return sqlite3_open_v2(filename, db, flags, NULL);
}

static inline int _close(sqlite3 *db) {
	return sqlite3_close_v2(db);
}

static inline int _exec(sqlite3 *db, const char *sql, char **errmsg) {
	return sqlite3_exec(db, sql, NULL, NULL, errmsg);
}

static inline int _prepare(sqlite3 *db, const char *sql, int nbyte, sqlite3_stmt **stmt, const char **tail) {
	return sqlite3_prepare_v2(db, sql, nbyte, stmt, tail);
}

static inline int _step(sqlite3_stmt *stmt) {
	return sqlite3_step(stmt);
}

static inline int _finalize(sqlite3_stmt *stmt) {
	return sqlite3_finalize(stmt);
}

static inline int _reset(sqlite3_stmt *stmt) {
	return sqlite3_reset(stmt);
}

static inline int _clear_bindings(sqlite3_stmt *stmt) {
	return sqlite3_clear_bindings(stmt);
}

static inline int _column_count(sqlite3_stmt *stmt) {
	return sqlite3_column_count(stmt);
}

static inline int _column_type(sqlite3_stmt *stmt, int col) {
	return sqlite3_column_type(stmt, col);
}

static inline const char *_column_name(sqlite3_stmt *stmt, int col) {
	return sqlite3_column_name(stmt, col);
}

static inline long long _column_int64(sqlite3_stmt *stmt, int col) {
	return sqlite3_column_int64(stmt, col);
}

static inline double _column_double(sqlite3_stmt *stmt, int col) {
	return sqlite3_column_double(stmt, col);
}

static inline const char *_column_text(sqlite3_stmt *stmt, int col) {
	return (const char *)sqlite3_column_text(stmt, col);
}

static inline const void *_column_blob(sqlite3_stmt *stmt, int col) {
	return sqlite3_column_blob(stmt, col);
}

static inline int _column_bytes(sqlite3_stmt *stmt, int col) {
	return sqlite3_column_bytes(stmt, col);
}

static inline int _bind_null(sqlite3_stmt *stmt, int col) {
	return sqlite3_bind_null(stmt, col);
}

static inline int _bind_int64(sqlite3_stmt *stmt, int col, long long val) {
	return sqlite3_bind_int64(stmt, col, val);
}

static inline int _bind_double(sqlite3_stmt *stmt, int col, double val) {
	return sqlite3_bind_double(stmt, col, val);
}

static inline int _bind_text(sqlite3_stmt *stmt, int col, const char *val, int n) {
	return sqlite3_bind_text(stmt, col, val, n, SQLITE_TRANSIENT);
}

static inline int _bind_blob(sqlite3_stmt *stmt, int col, const void *val, int n) {
	return sqlite3_bind_blob(stmt, col, val, n, SQLITE_TRANSIENT);
}

static inline const char *_errmsg(sqlite3 *db) {
	return sqlite3_errmsg(db);
}

static inline int _changes(sqlite3 *db) {
	return sqlite3_changes(db);
}

static inline long long _last_insert_rowid(sqlite3 *db) {
	return sqlite3_last_insert_rowid(db);
}

#endif
