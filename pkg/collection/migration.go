package collection

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/accretional/collector/gen/collector"
)

// Migrator handles migration from old database structure to new.
type Migrator struct {
	pathConfig    *PathConfig
	registryStore RegistryStore
	mu            sync.Mutex
}

// MigrationReport contains the results of a migration operation.
type MigrationReport struct {
	StartTime time.Time
	EndTime   time.Time
	Migrated  int
	Failed    int
	Errors    []string
}

// NewMigrator creates a new migration manager.
func NewMigrator(pathConfig *PathConfig, registryStore RegistryStore) *Migrator {
	return &Migrator{
		pathConfig:    pathConfig,
		registryStore: registryStore,
	}
}

// NeedsMigration checks if there are collections in the old structure.
func (m *Migrator) NeedsMigration(ctx context.Context) (bool, error) {
	oldBasePath := filepath.Join(m.pathConfig.DataDir, "collections")
	_, err := os.Stat(oldBasePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Check if there are any subdirectories
	entries, err := os.ReadDir(oldBasePath)
	if err != nil {
		return false, err
	}

	// Look for namespace directories
	for _, entry := range entries {
		if entry.IsDir() {
			// Check if this namespace has any collections
			nsPath := filepath.Join(oldBasePath, entry.Name())
			collections, err := os.ReadDir(nsPath)
			if err != nil {
				continue
			}
			if len(collections) > 0 {
				return true, nil
			}
		}
	}

	return false, nil
}

// MigrateAll migrates all collections from old structure to new.
func (m *Migrator) MigrateAll(ctx context.Context) (*MigrationReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	report := &MigrationReport{
		StartTime: time.Now(),
	}

	oldBasePath := filepath.Join(m.pathConfig.DataDir, "collections")

	// Check if old base path exists
	if _, err := os.Stat(oldBasePath); os.IsNotExist(err) {
		report.EndTime = time.Now()
		return report, nil
	}

	// Scan for namespaces
	namespaces, err := os.ReadDir(oldBasePath)
	if err != nil {
		return report, fmt.Errorf("failed to read collections directory: %w", err)
	}

	for _, ns := range namespaces {
		if !ns.IsDir() {
			continue
		}

		collectionsPath := filepath.Join(oldBasePath, ns.Name())
		collections, err := os.ReadDir(collectionsPath)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", ns.Name(), err))
			continue
		}

		for _, coll := range collections {
			if !coll.IsDir() {
				continue
			}

			// Skip .old directories (backups from previous migrations)
			if filepath.Ext(coll.Name()) == ".old" {
				continue
			}

			if err := m.migrateCollection(ctx, ns.Name(), coll.Name()); err != nil {
				report.Failed++
				report.Errors = append(report.Errors,
					fmt.Sprintf("%s/%s: %v", ns.Name(), coll.Name(), err))
				log.Printf("Warning: failed to migrate %s/%s: %v", ns.Name(), coll.Name(), err)
			} else {
				report.Migrated++
				log.Printf("Migrated collection %s/%s", ns.Name(), coll.Name())
			}
		}
	}

	report.EndTime = time.Now()
	return report, nil
}

// migrateCollection migrates a single collection.
func (m *Migrator) migrateCollection(ctx context.Context, namespace, name string) error {
	oldDBPath := m.pathConfig.OldCollectionDBPath(namespace, name)
	newDBPath := m.pathConfig.CollectionDBPath(namespace, name)

	// Check if old DB exists
	if _, err := os.Stat(oldDBPath); os.IsNotExist(err) {
		return fmt.Errorf("old database not found: %s", oldDBPath)
	}

	// Check if new DB already exists (skip if already migrated)
	if _, err := os.Stat(newDBPath); err == nil {
		log.Printf("Collection %s/%s already migrated, skipping", namespace, name)
		return nil
	}

	// Ensure new directory exists
	if err := os.MkdirAll(filepath.Dir(newDBPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Copy database to new location (atomic)
	if err := copyFileAtomic(oldDBPath, newDBPath); err != nil {
		return fmt.Errorf("failed to copy database: %w", err)
	}

	// Register in collections.db
	collection := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"migrated":       "true",
				"migration_time": time.Now().Format(time.RFC3339),
			},
		},
	}

	relativePath := fmt.Sprintf("%s/%s.db", namespace, name)
	if err := m.registryStore.SaveCollection(ctx, collection, relativePath); err != nil {
		// Rollback: remove copied DB
		os.Remove(newDBPath)
		return fmt.Errorf("failed to register in registry: %w", err)
	}

	// Rename old directory to .old as backup
	oldDirPath := filepath.Dir(oldDBPath)
	backupPath := oldDirPath + ".old"

	// If backup already exists, add a timestamp
	if _, err := os.Stat(backupPath); err == nil {
		backupPath = fmt.Sprintf("%s.old.%d", oldDirPath, time.Now().Unix())
	}

	if err := os.Rename(oldDirPath, backupPath); err != nil {
		log.Printf("Warning: failed to rename old directory to .old: %v", err)
	}

	return nil
}

// copyFileAtomic copies a file atomically using a temporary file and rename.
func copyFileAtomic(src, dst string) error {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	// Create temporary destination file
	tmpPath := dst + ".tmp"
	dstFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Copy content
	_, err = io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to copy data: %w", err)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename: %w", err)
	}

	return nil
}
