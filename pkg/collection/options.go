package collection

// Options configures the feature set for a Collection.
type Options struct {
	EnableFTS        bool
	EnableJSON       bool
	EnableVector     bool
	VectorDimensions int
}
