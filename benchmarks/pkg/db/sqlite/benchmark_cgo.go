//go:build benchmark && cgo
// +build benchmark,cgo

package sqlite

// Import mattn/go-sqlite3 for CGo comparison benchmarks
// This import is only included when both benchmark tag and cgo are enabled
import _ "github.com/mattn/go-sqlite3"

