package sqlite

/*
#include "cgo.h"
*/
import "C"

import (
	"fmt"
	"time"
)

// Tx is a database transaction. It holds the database mutex for its
// entire lifetime, so callers should commit or rollback promptly.
type Tx struct {
	db       *DB
	finished bool
}

// Begin starts a new transaction. The database mutex is held until
// Commit or Rollback is called.
func (db *DB) Begin() (*Tx, error) {
	db.mu.Lock()

	if db.db == nil {
		db.mu.Unlock()
		return nil, fmt.Errorf("sqlite: database not open")
	}

	if err := db.execLocked("BEGIN IMMEDIATE"); err != nil {
		db.mu.Unlock()
		return nil, fmt.Errorf("sqlite: begin: %w", err)
	}

	return &Tx{db: db}, nil
}

// Commit commits the transaction and releases the database mutex.
func (tx *Tx) Commit() error {
	if tx.finished {
		return fmt.Errorf("sqlite: transaction already finished")
	}
	tx.finished = true
	err := tx.db.execLocked("COMMIT")
	tx.db.mu.Unlock()
	if err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	return nil
}

// Rollback aborts the transaction and releases the database mutex.
// It is safe to call Rollback after Commit — it is a no-op.
func (tx *Tx) Rollback() error {
	if tx.finished {
		return nil
	}
	tx.finished = true
	err := tx.db.execLocked("ROLLBACK")
	tx.db.mu.Unlock()
	if err != nil {
		return fmt.Errorf("sqlite: rollback: %w", err)
	}
	return nil
}

// Exec executes a SQL statement within the transaction. Uses the write
// connection held by the transaction.
func (tx *Tx) Exec(sql string, args ...any) (Result, error) {
	start := time.Now()

	if tx.finished {
		return Result{}, fmt.Errorf("sqlite: transaction already finished")
	}

	conn := tx.db.db
	stmt, err := tx.db.prepare(conn, sql)
	if err != nil {
		tx.db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}
	defer C._finalize(stmt)

	if err := tx.db.bind(conn, stmt, args); err != nil {
		tx.db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}

	rc := C._step(stmt)
	if rc != resultDone && rc != resultRow {
		err := fmt.Errorf("sqlite: exec: %s", C.GoString(C._errmsg(conn)))
		tx.db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}

	result := Result{
		LastInsertID: int64(C._last_insert_rowid(conn)),
		RowsAffected: int64(C._changes(conn)),
	}
	tx.db.totalWrites.Add(1)
	tx.db.logQuery(sql, time.Since(start), nil)
	return result, nil
}

// Query executes a SQL query within the transaction. Uses the write
// connection held by the transaction. The caller must call Rows.Close
// when done. Note: Rows.Close does NOT release the transaction mutex
// — only Commit or Rollback does that.
func (tx *Tx) Query(sql string, args ...any) (*Rows, error) {
	start := time.Now()

	if tx.finished {
		return nil, fmt.Errorf("sqlite: transaction already finished")
	}

	conn := tx.db.db
	stmt, err := tx.db.prepare(conn, sql)
	if err != nil {
		tx.db.logQuery(sql, time.Since(start), err)
		return nil, err
	}

	if err := tx.db.bind(conn, stmt, args); err != nil {
		C._finalize(stmt)
		tx.db.logQuery(sql, time.Since(start), err)
		return nil, err
	}

	tx.db.totalReads.Add(1)
	return &Rows{
		db:   tx.db,
		conn: conn,
		mu:   nil, // transaction owns the write mutex
		stmt: stmt,
		start: start,
		sql:   sql,
	}, nil
}

// QueryRow executes a SQL query within the transaction that returns
// at most one row.
func (tx *Tx) QueryRow(sql string, args ...any) *Row {
	rows, err := tx.Query(sql, args...)
	if err != nil {
		return &Row{err: err}
	}
	return &Row{rows: rows}
}

// ExecMany executes a SQL statement for each set of args in a batch.
// The statement is prepared once and reused for each set of args.
func (tx *Tx) ExecMany(sql string, argSets [][]any) error {
	start := time.Now()

	if tx.finished {
		return fmt.Errorf("sqlite: transaction already finished")
	}

	conn := tx.db.db
	stmt, err := tx.db.prepare(conn, sql)
	if err != nil {
		tx.db.logQuery(sql, time.Since(start), err)
		return err
	}
	defer C._finalize(stmt)

	for i, args := range argSets {
		C._reset(stmt)
		C._clear_bindings(stmt)

		if err := tx.db.bind(conn, stmt, args); err != nil {
			err = fmt.Errorf("sqlite: exec batch item %d: %w", i, err)
			tx.db.logQuery(sql, time.Since(start), err)
			return err
		}

		rc := C._step(stmt)
		if rc != resultDone && rc != resultRow {
			err := fmt.Errorf("sqlite: exec batch item %d: %s", i, C.GoString(C._errmsg(conn)))
			tx.db.logQuery(sql, time.Since(start), err)
			return err
		}
	}
	tx.db.totalWrites.Add(int64(len(argSets)))
	tx.db.logQuery(sql, time.Since(start), nil)
	return nil
}

// InTx runs fn inside a transaction. If fn returns nil, the
// transaction is committed. If fn returns an error or panics,
// the transaction is rolled back.
func (db *DB) InTx(fn func(*Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
