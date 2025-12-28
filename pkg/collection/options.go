package collection

// JSONConverterType identifies the strategy for converting binary protobuf to JSON.
// This enum is serializable; the actual converter function is wired at runtime.
type JSONConverterType int

const (
	// JSONConverterNone means no conversion - proto_data must already be JSON
	JSONConverterNone JSONConverterType = iota
	// JSONConverterDynamic uses the registry to look up the proto type and convert dynamically
	JSONConverterDynamic
)

// ProtoToJSONConverter converts binary protobuf data to a JSON string for search indexing.
// Returns empty string if conversion is not possible (e.g., unknown type).
type ProtoToJSONConverter func(protoData []byte) (string, error)

// JSONConverterFactory creates converters based on collection type information.
// This allows the repo to get the appropriate converter for any collection.
type JSONConverterFactory func(namespace, messageName string) ProtoToJSONConverter

// Options configures the feature set for a Collection's Store.
type Options struct {
	EnableFTS         bool
	EnableJSON        bool
	EnableVector      bool
	VectorDimensions  int
	JSONConverterType JSONConverterType // Strategy for proto→JSON conversion (serializable)
}
