package collection

// Options configures the feature set for a Collection.
type Options struct {
	EnableFTS        bool
	EnableJSON       bool
	EnableVector     bool
	VectorDimensions int
	Embedder         Embedder // Required when EnableVector is true
}

// ProtoToJSONConverter is a function that converts binary protobuf data to JSON string.
// It should return a valid JSON representation of the protobuf message.
type ProtoToJSONConverter func(protoData []byte) (string, error)

// JSONConverterType specifies which type of JSON converter to use
type JSONConverterType int

const (
	// JSONConverterNone means no converter is set (uses raw data if valid JSON)
	JSONConverterNone JSONConverterType = iota
	// JSONConverterStatic uses a compile-time known proto type
	JSONConverterStatic
	// JSONConverterDynamic uses dynamic type lookup from registry
	JSONConverterDynamic
)

// JSONConverterFactory creates ProtoToJSONConverter based on collection type
type JSONConverterFactory func(namespace, collectionName string) ProtoToJSONConverter
