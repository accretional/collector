package collection

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
