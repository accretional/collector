package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TypeRegistry provides type validation for collections
// It tracks all registered message types and validates that collections
// reference valid types
type TypeRegistry struct {
	typesCollection *collection.Collection
	cache           map[string]*pb.ValidationRule
	mu              sync.RWMutex
}

// NewTypeRegistry creates a new TypeRegistry wrapping the system/types collection
func NewTypeRegistry(typesCollection *collection.Collection) *TypeRegistry {
	return &TypeRegistry{
		typesCollection: typesCollection,
		cache:           make(map[string]*pb.ValidationRule),
	}
}

// RegisterMessageType registers a message type from a FileDescriptor
// This should be called when RegisterProto is called in the registry
func (tr *TypeRegistry) RegisterMessageType(ctx context.Context, namespace string, messageDesc *descriptorpb.DescriptorProto, fileDesc *descriptorpb.FileDescriptorProto) error {
	if messageDesc == nil {
		return fmt.Errorf("message descriptor cannot be nil")
	}
	if messageDesc.GetName() == "" {
		return fmt.Errorf("message name is required")
	}

	messageName := messageDesc.GetName()
	typeID := fmt.Sprintf("%s/%s", namespace, messageName)

	// Check if already registered
	_, err := tr.typesCollection.GetRecord(ctx, typeID)
	if err == nil {
		// Already exists, update it
		return tr.updateMessageType(ctx, typeID, namespace, messageName, messageDesc, fileDesc)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("check existing type: %w", err)
	}

	// Create new registration
	// Extract field information for validation
	fieldRules := []*pb.ValidationRule_FieldRule{}
	for _, field := range messageDesc.Field {
		fieldRule := &pb.ValidationRule_FieldRule{
			FieldName: field.GetName(),
			// TODO: Add more sophisticated validation rules based on field type
			// For now, just track field names
		}
		fieldRules = append(fieldRules, fieldRule)
	}

	validationRule := &pb.ValidationRule{
		Id:          typeID,
		Namespace:   namespace,
		MessageName: messageName,
		FieldRules:  fieldRules,
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"proto_file": fileDesc.GetName(),
			},
		},
	}

	ruleBytes, err := proto.Marshal(validationRule)
	if err != nil {
		return fmt.Errorf("marshal validation rule: %w", err)
	}

	record := &pb.CollectionRecord{
		Id:        typeID,
		ProtoData: ruleBytes,
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"namespace":  namespace,
				"proto_file": fileDesc.GetName(),
			},
		},
	}

	if err := tr.typesCollection.CreateRecord(ctx, record); err != nil {
		return fmt.Errorf("create type record: %w", err)
	}

	// Update cache
	tr.mu.Lock()
	tr.cache[typeID] = validationRule
	tr.mu.Unlock()

	return nil
}

// updateMessageType updates an existing message type registration
func (tr *TypeRegistry) updateMessageType(ctx context.Context, typeID, namespace, messageName string, messageDesc *descriptorpb.DescriptorProto, fileDesc *descriptorpb.FileDescriptorProto) error {
	fieldRules := []*pb.ValidationRule_FieldRule{}
	for _, field := range messageDesc.Field {
		fieldRule := &pb.ValidationRule_FieldRule{
			FieldName: field.GetName(),
		}
		fieldRules = append(fieldRules, fieldRule)
	}

	validationRule := &pb.ValidationRule{
		Id:          typeID,
		Namespace:   namespace,
		MessageName: messageName,
		FieldRules:  fieldRules,
		Metadata: &pb.Metadata{
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"proto_file": fileDesc.GetName(),
			},
		},
	}

	ruleBytes, err := proto.Marshal(validationRule)
	if err != nil {
		return fmt.Errorf("marshal validation rule: %w", err)
	}

	record := &pb.CollectionRecord{
		Id:        typeID,
		ProtoData: ruleBytes,
		Metadata: &pb.Metadata{
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"namespace":  namespace,
				"proto_file": fileDesc.GetName(),
			},
		},
	}

	if err := tr.typesCollection.UpdateRecord(ctx, record); err != nil {
		return fmt.Errorf("update type record: %w", err)
	}

	// Update cache
	tr.mu.Lock()
	tr.cache[typeID] = validationRule
	tr.mu.Unlock()

	return nil
}

// RegisterFileDescriptor registers all message types from a FileDescriptor
// This is the main entry point when RegisterProto is called
func (tr *TypeRegistry) RegisterFileDescriptor(ctx context.Context, namespace string, fileDesc *descriptorpb.FileDescriptorProto) error {
	if fileDesc == nil {
		return fmt.Errorf("file descriptor cannot be nil")
	}

	// Register all message types in the file
	for _, messageDesc := range fileDesc.MessageType {
		if err := tr.RegisterMessageType(ctx, namespace, messageDesc, fileDesc); err != nil {
			return fmt.Errorf("register message %s: %w", messageDesc.GetName(), err)
		}
	}

	return nil
}

// ValidateMessageType checks if a message type is registered
func (tr *TypeRegistry) ValidateMessageType(ctx context.Context, namespace, messageName string) error {
	typeID := fmt.Sprintf("%s/%s", namespace, messageName)
	_, err := tr.GetMessageType(ctx, namespace, messageName)
	if err != nil {
		return fmt.Errorf("message type %s not registered: %w", typeID, err)
	}
	return nil
}

// ValidateCollectionMessageType validates that a collection's message type is registered
func (tr *TypeRegistry) ValidateCollectionMessageType(ctx context.Context, coll *pb.Collection) error {
	if coll == nil {
		return fmt.Errorf("collection cannot be nil")
	}

	if coll.MessageType == nil {
		// Collections without a message type are allowed (untyped collections)
		return nil
	}

	// Validate the message type is registered
	return tr.ValidateMessageType(ctx, coll.MessageType.Namespace, coll.MessageType.MessageName)
}

// GetMessageType retrieves a registered message type's validation rule
func (tr *TypeRegistry) GetMessageType(ctx context.Context, namespace, messageName string) (*pb.ValidationRule, error) {
	typeID := fmt.Sprintf("%s/%s", namespace, messageName)

	// Check cache first
	tr.mu.RLock()
	if rule, ok := tr.cache[typeID]; ok {
		tr.mu.RUnlock()
		return rule, nil
	}
	tr.mu.RUnlock()

	// Cache miss - look up in collection
	record, err := tr.typesCollection.GetRecord(ctx, typeID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("message type %s not found", typeID)
	}
	if err != nil {
		return nil, fmt.Errorf("get message type: %w", err)
	}

	var rule pb.ValidationRule
	if err := proto.Unmarshal(record.ProtoData, &rule); err != nil {
		return nil, fmt.Errorf("unmarshal validation rule: %w", err)
	}

	// Update cache
	tr.mu.Lock()
	tr.cache[typeID] = &rule
	tr.mu.Unlock()

	return &rule, nil
}

// ListMessageTypes returns all registered message types, optionally filtered by namespace
func (tr *TypeRegistry) ListMessageTypes(ctx context.Context, namespace string) ([]*pb.ValidationRule, error) {
	// TODO: Use search/filter when available
	records, err := tr.typesCollection.ListRecords(ctx, 0, 10000)
	if err != nil {
		return nil, fmt.Errorf("list type records: %w", err)
	}

	var rules []*pb.ValidationRule
	for _, record := range records {
		var rule pb.ValidationRule
		if err := proto.Unmarshal(record.ProtoData, &rule); err != nil {
			return nil, fmt.Errorf("unmarshal validation rule: %w", err)
		}

		// Filter by namespace if specified
		if namespace == "" || rule.Namespace == namespace {
			rules = append(rules, &rule)
		}
	}

	return rules, nil
}

// CountMessageTypes returns the number of registered message types
func (tr *TypeRegistry) CountMessageTypes(ctx context.Context) (int64, error) {
	return tr.typesCollection.CountRecords(ctx)
}
