package collection

import (
	"fmt"
	"strings"
)

// Reserved namespaces that cannot be used by users
// These are reserved because they conflict with internal directory structure
var reservedNamespaces = map[string]bool{
	"repo":    true, // Used for ./data/repo/collections.db
	"backups": true, // Used for ./data/backups/metadata.db
	"files":   true, // Used for ./data/files/{namespace}/{name}
	// Note: "system", "internal", "admin", "metadata" are intentionally NOT reserved
	// They are valid namespaces used for system collections
	// Note: .backup is blocked by the "no leading dot" rule in ValidateName
}

// Maximum length for namespace and collection names
const (
	MaxNameLength = 255
	MinNameLength = 1
)

// Invalid characters in names (filesystem-unsafe characters)
// Note: "/" is NOT allowed - no hierarchical namespaces supported
const invalidChars = `./\:*?"<>|`

// ValidateName validates a namespace or collection name.
// Returns an error if the name is invalid.
func ValidateName(name string, fieldName string) error {
	// Check length
	if len(name) < MinNameLength {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if len(name) > MaxNameLength {
		return fmt.Errorf("%s exceeds maximum length of %d characters", fieldName, MaxNameLength)
	}

	// Check for invalid characters
	for _, char := range invalidChars {
		if strings.ContainsRune(name, char) {
			return fmt.Errorf("%s contains invalid character '%c'", fieldName, char)
		}
	}

	// Reject names starting with dot (hidden files/directories)
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("%s cannot start with '.'", fieldName)
	}

	// Check for path traversal patterns
	if strings.Contains(name, "..") {
		return fmt.Errorf("%s contains path traversal pattern '..'", fieldName)
	}

	// Reject whitespace-only names
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s cannot be whitespace-only", fieldName)
	}

	return nil
}

// ValidateNamespace validates a namespace name and checks if it's reserved.
func ValidateNamespace(namespace string) error {
	if err := ValidateName(namespace, "namespace"); err != nil {
		return err
	}

	// Check if namespace is reserved
	if reservedNamespaces[namespace] {
		return fmt.Errorf("namespace '%s' is reserved and cannot be used", namespace)
	}

	return nil
}

// ValidateCollectionName validates a collection name.
func ValidateCollectionName(name string) error {
	return ValidateName(name, "collection name")
}

// ValidateServiceName validates a service name (rejects slashes which break ID format).
func ValidateServiceName(name string, fieldName string) error {
	// First apply standard validation
	if err := ValidateName(name, fieldName); err != nil {
		return err
	}

	// Reject slash (breaks namespace/servicename ID format)
	if strings.Contains(name, "/") {
		return fmt.Errorf("%s cannot contain '/' (breaks ID format)", fieldName)
	}

	return nil
}

// ValidateProtoFileName validates a proto file name (allows dots for .proto extension).
func ValidateProtoFileName(name string, fieldName string) error {
	// Check length
	if len(name) < MinNameLength {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if len(name) > MaxNameLength {
		return fmt.Errorf("%s exceeds maximum length of %d characters", fieldName, MaxNameLength)
	}

	// Invalid characters for proto names (allow dots for .proto extension)
	const invalidProtoChars = `\:*?"<>|`
	for _, char := range invalidProtoChars {
		if strings.ContainsRune(name, char) {
			return fmt.Errorf("%s contains invalid character '%c'", fieldName, char)
		}
	}

	// Reject names starting with dot (hidden files)
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("%s cannot start with '.'", fieldName)
	}

	// Check for path traversal patterns
	if strings.Contains(name, "..") {
		return fmt.Errorf("%s contains path traversal pattern '..'", fieldName)
	}

	// Reject slash (breaks namespace/name ID format)
	if strings.Contains(name, "/") {
		return fmt.Errorf("%s cannot contain '/' (breaks ID format)", fieldName)
	}

	// Reject whitespace-only names
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s cannot be whitespace-only", fieldName)
	}

	return nil
}

// IsReservedNamespace checks if a namespace is reserved.
func IsReservedNamespace(namespace string) bool {
	return reservedNamespaces[namespace]
}
