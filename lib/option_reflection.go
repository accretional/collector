package reflection

import (
	"fmt"
	
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	// We import the generated code for the meta-schema
	resv1 "github.com/yourorg/rpc/gen/platform/resource/v1" 
)

// ArchetypeMeta represents the extracted data ready for the UI.
type ArchetypeMeta struct {
	Name        string
	Description string
	Defaults    string // JSON string
}

// FieldTrait represents a UI hint for a specific field.
type FieldTrait struct {
	FieldName string
	Trait     string // e.g. "geographic_location"
}

// IntrospectArchetypes inspects a Protobuf Message struct at runtime
// and extracts the "Presets" option defined in the .proto file.
func IntrospectArchetypes(msg proto.Message) ([]ArchetypeMeta, error) {
	// 1. Get the Message Descriptor (Runtime reflection)
	desc := msg.ProtoReflect().Descriptor()
	
	// 2. Access the Options message
	// This corresponds to `message CreateInstanceRequest { option (...) }`
	options := desc.Options().(*descriptorpb.MessageOptions)

	// 3. Extract our custom Extension (E_Presets)
	// The Go generator created `E_Presets` based on the `extend MessageOptions` block.
	if !proto.HasExtension(options, resv1.E_Presets) {
		return nil, nil 
	}

	// 4. Cast and Convert
	// The extension returns the slice of ArchetypePreset messages.
	extVal := proto.GetExtension(options, resv1.E_Presets)
	presets, ok := extVal.([]*resv1.ArchetypePreset)
	if !ok {
		return nil, fmt.Errorf("failed to cast presets extension")
	}

	// 5. Map to our internal struct
	var results []ArchetypeMeta
	for _, p := range presets {
		results = append(results, ArchetypeMeta{
			Name:        p.Name,
			Description: p.Doc,
			Defaults:    p.DefaultsJson,
		})
	}

	return results, nil
}

// IntrospectTraits walks the fields of the message to find UI hints.
func IntrospectTraits(msg proto.Message) ([]FieldTrait, error) {
	desc := msg.ProtoReflect().Descriptor()
	fields := desc.Fields()

	var results []FieldTrait

	// Walk every field in the message
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		opts := field.Options().(*descriptorpb.FieldOptions)

		// Check for our custom `trait` extension
		if proto.HasExtension(opts, resv1.E_Trait) {
			traitName := proto.GetExtension(opts, resv1.E_Trait).(string)
			results = append(results, FieldTrait{
				FieldName: string(field.Name()),
				Trait:     traitName,
			})
		}
	}

	return results, nil
}
