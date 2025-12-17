//go:build !cgo

package sqliteext

import (
	"context"
	"strings"
	"testing"
)

func TestStubOpenReturnsError(t *testing.T) {
	ctx := context.Background()

	db, err := Open(ctx, "/tmp/test.db", "/path/to/extension.so", "init")
	if err == nil {
		t.Fatal("expected error when opening with stub driver, got nil")
	}
	if db != nil {
		t.Fatal("expected nil db when opening with stub driver")
	}

	if !strings.Contains(err.Error(), "CGo") {
		t.Errorf("expected error to mention CGo, got: %v", err)
	}
}

func TestStubCloseNoError(t *testing.T) {
	var db DB
	if err := db.Close(); err != nil {
		t.Errorf("stub Close should not error, got: %v", err)
	}
}

func TestStubConnReturnsNil(t *testing.T) {
	var db DB
	if conn := db.Conn(); conn != nil {
		t.Error("stub Conn should return nil")
	}
}

func TestStubQueryContextReturnsError(t *testing.T) {
	ctx := context.Background()
	var db DB

	rows, err := db.QueryContext(ctx, "SELECT 1")
	if err == nil {
		t.Fatal("expected error from stub QueryContext")
	}
	if rows != nil {
		t.Fatal("expected nil rows from stub QueryContext")
	}
}

func TestStubExecContextReturnsError(t *testing.T) {
	ctx := context.Background()
	var db DB

	result, err := db.ExecContext(ctx, "SELECT 1")
	if err == nil {
		t.Fatal("expected error from stub ExecContext")
	}
	if result != nil {
		t.Fatal("expected nil result from stub ExecContext")
	}
}
