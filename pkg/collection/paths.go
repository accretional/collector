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
func (pc *PathConfig) CollectionDBPath(namespace, name string) string {
	return filepath.Join(pc.DataDir, namespace, name+".db")
}

// CollectionFilesPath returns the path for a collection's file storage.
// Format: {DataDir}/files/{namespace}/{name}
func (pc *PathConfig) CollectionFilesPath(namespace, name string) string {
	return filepath.Join(pc.DataDir, "files", namespace, name)
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

// OldCollectionDBPath returns the path for a collection in the old structure.
// Used for migration detection and compatibility.
// Format: {DataDir}/collections/{namespace}/{name}/data.db
func (pc *PathConfig) OldCollectionDBPath(namespace, name string) string {
	return filepath.Join(pc.DataDir, "collections", namespace, name, "data.db")
}

// HasOldStructure checks if a collection exists in the old directory structure.
func (pc *PathConfig) HasOldStructure(namespace, name string) bool {
	oldPath := pc.OldCollectionDBPath(namespace, name)
	_, err := os.Stat(oldPath)
	return err == nil
}

// BackupDir returns the base backup directory.
// Format: {DataDir}/.backup
func (pc *PathConfig) BackupDir() string {
	return filepath.Join(pc.DataDir, ".backup")
}

// BackupPath generates a backup file path for a collection.
// Format: {DataDir}/.backup/{namespace}/{collectionname}-{timestampseconds}.db
func (pc *PathConfig) BackupPath(namespace, name string, timestamp int64) string {
	filename := fmt.Sprintf("%s-%d.db", name, timestamp)
	return filepath.Join(pc.BackupDir(), namespace, filename)
}

// BackupFilesPath generates a backup files directory path.
// Format: {DataDir}/.backup/{namespace}/{collectionname}-{timestampseconds}.files
func (pc *PathConfig) BackupFilesPath(namespace, name string, timestamp int64) string {
	filename := fmt.Sprintf("%s-%d.files", name, timestamp)
	return filepath.Join(pc.BackupDir(), namespace, filename)
}

// ListBackupPaths lists all backup database files for a collection.
// Returns sorted list (oldest to newest).
func (pc *PathConfig) ListBackupPaths(namespace, name string) ([]string, error) {
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
