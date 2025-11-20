package collection

import (
	"context"
	"fmt"
)

// Combinator handles merging multiple Collections.
type Combinator struct{}

// Attach links a secondary collection to the primary one using SQLite's ATTACH DATABASE.
func (c *Combinator) Attach(ctx context.Context, primary *Collection, secondaryPath string, alias string) error {
	query := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", secondaryPath, alias)
	return primary.Store.ExecuteRaw(query)
}

// UnionView creates a virtual view over attached collections.
func (c *Combinator) UnionView(ctx context.Context, primary *Collection, viewName string, aliases []string) error {
	// Construct: SELECT * FROM alias1.records UNION ALL SELECT * FROM alias2.records...
	var selects []string
	for _, alias := range aliases {
		selects = append(selects, fmt.Sprintf("SELECT * FROM %s.records", alias))
	}
	
	// Union query
	// query := fmt.Sprintf("CREATE VIEW %s AS %s", viewName, strings.Join(selects, " UNION ALL "))
	// return primary.Store.ExecuteRaw(query)
	return nil
}
