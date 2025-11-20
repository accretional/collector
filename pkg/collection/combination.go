package collection

// Combinator handles merging multiple Collections/Tables.
// Useful for "MapReduce" style aggregation where multiple workers produce
// partial SQLite DBs, and we need to view them as one.
type Combinator struct{}

// Attach links a secondary collection to the primary one using SQLite's ATTACH DATABASE.
// This allows performing joins across two separate collection files.
func (c *Combinator) Attach(primary *Collection, secondaryPath string, alias string) error {
	// Requires the Store interface to expose a way to execute generic commands
	// "ATTACH DATABASE ? AS ?"
	return nil
}

// Merge physically copies records from source collections into a destination collection.
func (c *Combinator) Merge(ctx context.Context, dest *Collection, sources ...*Collection) error {
	// 1. Attach all sources to dest
	// 2. INSERT INTO dest.records SELECT * FROM sourceN.records
	// 3. Detach
	return nil
}

// UnionView creates a virtual view over multiple attached collections.
// "CREATE VIEW unified_records AS SELECT * FROM db1.records UNION ALL SELECT * FROM db2.records"
func (c *Combinator) UnionView(primary *Collection, aliases []string) error {
	return nil
}
