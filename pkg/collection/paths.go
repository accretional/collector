package collection

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// PathConfig provides centralized path management for collection storage.
// It allows configurable base directory and consistent path generation.
type PathConfig struct {
	DataDir string // Base data directory (default: "./data")
}

// NewPathConfig creates a new PathConfig with the specified data directory.
func NewPathConfig(dataDir string) *PathConfig {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &PathConfig{
		DataDir: dataDir,
	}
}

// CollectionDBPath returns the path for a collection's database file.
// Format: {DataDir}/{namespace}/{name}.db
func (pc *PathConfig) CollectionDBPath(namespace, name string) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	return filepath.Join(pc.DataDir, namespace, name+".db"), nil
}

// CollectionFilesPath returns the path for a collection's file storage.
// Format: {DataDir}/files/{namespace}/{name}
func (pc *PathConfig) CollectionFilesPath(namespace, name string) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	return filepath.Join(pc.DataDir, "files", namespace, name), nil
}

// RegistryDBPath returns the path for the collection registry database.
// Format: {DataDir}/repo/collections.db
func (pc *PathConfig) RegistryDBPath() string {
	return filepath.Join(pc.DataDir, "repo", "collections.db")
}

// BackupsMetadataPath returns the path for backup metadata database.
// Format: {DataDir}/backups/metadata.db
func (pc *PathConfig) BackupsMetadataPath() string {
	return filepath.Join(pc.DataDir, "backups", "metadata.db")
}

// BackupDir returns the base backup directory.
// Format: {DataDir}/.backup
func (pc *PathConfig) BackupDir() string {
	return filepath.Join(pc.DataDir, ".backup")
}

// BackupPath generates a backup file path for a collection.
// Format: {DataDir}/.backup/{namespace}/{collectionname}-{timestampseconds}.db
func (pc *PathConfig) BackupPath(namespace, name string, timestamp int64) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%d.db", name, timestamp)
	return filepath.Join(pc.BackupDir(), namespace, filename), nil
}

// BackupFilesPath generates a backup files directory path.
// Format: {DataDir}/.backup/{namespace}/{collectionname}-{timestampseconds}.files
func (pc *PathConfig) BackupFilesPath(namespace, name string, timestamp int64) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%d.files", name, timestamp)
	return filepath.Join(pc.BackupDir(), namespace, filename), nil
}

// BackupPathMicro generates a backup file path using microsecond precision.
// Format: {DataDir}/.backup/{namespace}/{collectionname}-{timestampmicroseconds}.db
// This prevents filesystem path collisions when multiple backups occur rapidly.
func (pc *PathConfig) BackupPathMicro(namespace, name string, timestampMicro int64) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%d.db", name, timestampMicro)
	return filepath.Join(pc.BackupDir(), namespace, filename), nil
}

// BackupFilesPathMicro generates a backup files directory path using microsecond precision.
// Format: {DataDir}/.backup/{namespace}/{collectionname}-{timestampmicroseconds}.files
func (pc *PathConfig) BackupFilesPathMicro(namespace, name string, timestampMicro int64) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%d.files", name, timestampMicro)
	return filepath.Join(pc.BackupDir(), namespace, filename), nil
}

// ListBackupPaths lists all backup database files for a collection.
// Returns sorted list (oldest to newest).
func (pc *PathConfig) ListBackupPaths(namespace, name string) ([]string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	if err := ValidateCollectionName(name); err != nil {
		return nil, err
	}

	backupDir := filepath.Join(pc.BackupDir(), namespace)
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil, nil
	}

	pattern := filepath.Join(backupDir, name+"-*.db")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	sort.Strings(matches)
	return matches, nil
}
