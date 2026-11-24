package configstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// A lock held by another connection must read as busy, and any other failure
// must not: the caller retries the first and fails closed on the second.
func TestIsBusyErrorFollowsTheDriverResultCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	open := func() *sql.DB {
		db, err := sql.Open("sqlite", dsnPath(path)+"?_pragma=busy_timeout(1)")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		return db
	}
	holder, contender := open(), open()
	ctx := context.Background()

	conn, err := holder.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	_, err = contender.ExecContext(ctx, "BEGIN IMMEDIATE")
	if err == nil || !isBusyError(err) {
		t.Fatalf("a held write lock should read as busy, got %v", err)
	}
	if isBusyError(errors.New("database is locked")) {
		t.Fatal("a plain error that only mentions a lock is not the driver's busy code")
	}
	if _, err := contender.ExecContext(ctx, "SELECT * FROM missing_table"); err == nil || isBusyError(err) {
		t.Fatalf("a missing table is not busy: %v", err)
	}
}
