package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/accretional/collector/pkg/collection"
	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestBackupConcurrentReads verifies that reads work during backup
func TestBackupConcurrentReads(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store with test data
	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert test records
	numRecords := 10000
	for i := 0; i < numRecords; i++ {
		record := &pb.CollectionRecord{
			Id:  fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Track read operations during backup
	var readsCompleted atomic.Int64
	var readErrors atomic.Int64
	var backupComplete atomic.Bool

	// Start concurrent readers
	numReaders := 10
	var wg sync.WaitGroup

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for !backupComplete.Load() {
				// Try to read a random record
				recordID := fmt.Sprintf("record-%d", readerID*1000%numRecords)
				_, err := store.GetRecord(ctx, recordID)
				if err != nil {
					readErrors.Add(1)
					t.Logf("Reader %d: failed to read during backup: %v", readerID, err)
				} else {
					readsCompleted.Add(1)
				}

				// Small delay to simulate real reads
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	// Give readers time to start
	time.Sleep(50 * time.Millisecond)

	// Perform backup
	backupPath := filepath.Join(tmpDir, "backup.db")
	backupStart := time.Now()

	if err := store.Backup(ctx, backupPath); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	backupDuration := time.Since(backupStart)
	backupComplete.Store(true)

	// Wait for readers to finish
	wg.Wait()

	// Verify results
	totalReads := readsCompleted.Load()
	totalErrors := readErrors.Load()

	t.Logf("Backup completed in %v", backupDuration)
	t.Logf("Concurrent reads completed: %d", totalReads)
	t.Logf("Read errors: %d", totalErrors)

	if totalReads == 0 {
		t.Error("No reads completed during backup - possible blocking!")
	}

	if totalErrors > 0 {
		t.Errorf("Read errors occurred during backup: %d/%d", totalErrors, totalReads)
	}

	// Verify backup is valid
	backupStore, err := NewSqliteStore(backupPath, collection.Options{})
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	defer backupStore.Close()

	count, err := backupStore.CountRecords(ctx)
	if err != nil {
		t.Fatalf("failed to count backup records: %v", err)
	}

	if count != int64(numRecords) {
		t.Errorf("backup has wrong number of records: got %d, want %d", count, numRecords)
	}
}

// TestBackupConcurrentWrites verifies that writes work during backup
func TestBackupConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store with initial data
	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert initial records
	initialRecords := 5000
	for i := 0; i < initialRecords; i++ {
		record := &pb.CollectionRecord{
			Id:  fmt.Sprintf("initial-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Track write operations during backup
	var writesCompleted atomic.Int64
	var writeErrors atomic.Int64
	var backupComplete atomic.Bool

	// Start concurrent writers
	numWriters := 5
	var wg sync.WaitGroup

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			writeCount := 0
			for !backupComplete.Load() {
				// Write a new record
				recordID := fmt.Sprintf("writer-%d-record-%d", writerID, writeCount)
				record := &pb.CollectionRecord{
					Id:  recordID,
					Metadata: &pb.Metadata{
						Labels:    map[string]string{},
						CreatedAt: timestamppb.Now(),
						UpdatedAt: timestamppb.Now(),
					},
					ProtoData: []byte(fmt.Sprintf("concurrent-write-%d", writeCount)),
				}

				err := store.CreateRecord(ctx, record)
				if err != nil {
					writeErrors.Add(1)
					t.Logf("Writer %d: failed to write during backup: %v", writerID, err)
				} else {
					writesCompleted.Add(1)
					writeCount++
				}

				// Small delay to simulate real writes
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	// Give writers time to start
	time.Sleep(50 * time.Millisecond)

	// Perform backup while writes are happening
	backupPath := filepath.Join(tmpDir, "backup.db")
	backupStart := time.Now()

	if err := store.Backup(ctx, backupPath); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	backupDuration := time.Since(backupStart)
	backupComplete.Store(true)

	// Wait for writers to finish
	wg.Wait()

	// Verify results
	totalWrites := writesCompleted.Load()
	totalErrors := writeErrors.Load()

	t.Logf("Backup completed in %v", backupDuration)
	t.Logf("Concurrent writes completed: %d", totalWrites)
	t.Logf("Write errors: %d", totalErrors)

	if totalWrites == 0 {
		t.Error("No writes completed during backup - writes were blocked!")
	}

	if totalErrors > 0 {
		t.Errorf("Write errors occurred during backup: %d/%d", totalErrors, totalWrites)
	}

	// Verify backup is consistent (should have initial records, may not have concurrent writes)
	backupStore, err := NewSqliteStore(backupPath, collection.Options{})
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	defer backupStore.Close()

	count, err := backupStore.CountRecords(ctx)
	if err != nil {
		t.Fatalf("failed to count backup records: %v", err)
	}

	// Backup should have at least the initial records
	if count < int64(initialRecords) {
		t.Errorf("backup has too few records: got %d, want at least %d", count, initialRecords)
	}

	t.Logf("Backup contains %d records (initial: %d, during backup: %d)",
		count, initialRecords, count-int64(initialRecords))
}

// TestBackupLockDuration measures actual lock time during backup
func TestBackupLockDuration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store with various data sizes
	testCases := []struct {
		name       string
		numRecords int
		maxLockMs  int64
	}{
		{"small-100", 100, 50},
		{"medium-1000", 1000, 100},
		{"large-10000", 10000, 200},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
			if err != nil {
				t.Fatalf("failed to create store: %v", err)
			}
			defer store.Close()
			defer os.Remove(dbPath)

			// Insert records
			for i := 0; i < tc.numRecords; i++ {
				record := &pb.CollectionRecord{
					Id:  fmt.Sprintf("record-%d", i),
					Metadata: &pb.Metadata{
						Labels:    map[string]string{},
						CreatedAt: timestamppb.Now(),
						UpdatedAt: timestamppb.Now(),
					},
					ProtoData: []byte(fmt.Sprintf("data-%d-with-some-extra-content-to-make-it-larger", i)),
				}
				if err := store.CreateRecord(ctx, record); err != nil {
					t.Fatalf("failed to create record: %v", err)
				}
			}

			// Measure lock duration by attempting concurrent write
			var maxBlockTime atomic.Int64
			var attemptedWrites atomic.Int64
			done := make(chan struct{})

			// Writer goroutine that will be blocked if locks are held
			go func() {
				for {
					select {
					case <-done:
						return
					default:
						writeStart := time.Now()
						record := &pb.CollectionRecord{
							Id:  fmt.Sprintf("probe-%d", time.Now().UnixNano()),
							Metadata: &pb.Metadata{
								Labels:    map[string]string{},
								CreatedAt: timestamppb.Now(),
								UpdatedAt: timestamppb.Now(),
							},
							ProtoData: []byte("probe"),
						}
						err := store.CreateRecord(ctx, record)
						writeDuration := time.Since(writeStart).Milliseconds()

						if err == nil {
							attemptedWrites.Add(1)
							if writeDuration > maxBlockTime.Load() {
								maxBlockTime.Store(writeDuration)
							}
						}

						time.Sleep(1 * time.Millisecond)
					}
				}
			}()

			// Perform backup
			backupPath := filepath.Join(tmpDir, fmt.Sprintf("backup-%s.db", tc.name))
			backupStart := time.Now()

			if err := store.Backup(ctx, backupPath); err != nil {
				t.Fatalf("backup failed: %v", err)
			}

			backupDuration := time.Since(backupStart)
			close(done)
			time.Sleep(10 * time.Millisecond) // Let probe finish

			maxLock := maxBlockTime.Load()
			t.Logf("Backup duration: %v", backupDuration)
			t.Logf("Max write block time: %d ms", maxLock)
			t.Logf("Probe writes during backup: %d", attemptedWrites.Load())

			if maxLock > tc.maxLockMs {
				t.Errorf("Lock time too high: %d ms > %d ms threshold", maxLock, tc.maxLockMs)
			}
		})
	}
}

// TestBackupConsistency verifies backup data integrity
func TestBackupConsistency(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert records with known checksums
	numRecords := 1000
	expectedData := make(map[string]string)

	for i := 0; i < numRecords; i++ {
		recordID := fmt.Sprintf("record-%d", i)
		data := fmt.Sprintf("data-content-%d", i)
		expectedData[recordID] = data

		record := &pb.CollectionRecord{
			Id:  recordID,
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(data),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Perform backup
	backupPath := filepath.Join(tmpDir, "backup.db")
	if err := store.Backup(ctx, backupPath); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	// Open backup and verify all data
	backupStore, err := NewSqliteStore(backupPath, collection.Options{})
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	defer backupStore.Close()

	// Verify count
	count, err := backupStore.CountRecords(ctx)
	if err != nil {
		t.Fatalf("failed to count records: %v", err)
	}
	if count != int64(numRecords) {
		t.Errorf("wrong record count: got %d, want %d", count, numRecords)
	}

	// Verify each record
	for recordID, expectedContent := range expectedData {
		record, err := backupStore.GetRecord(ctx, recordID)
		if err != nil {
			t.Errorf("failed to get record %s: %v", recordID, err)
			continue
		}

		if string(record.ProtoData) != expectedContent {
			t.Errorf("record %s: got %s, want %s", recordID, record.ProtoData, expectedContent)
		}
	}
}

// TestBackupUnderLoad simulates realistic production load
func TestBackupUnderLoad(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Pre-populate database
	for i := 0; i < 5000; i++ {
		record := &pb.CollectionRecord{
			Id:  fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		store.CreateRecord(ctx, record)
	}

	// Statistics
	var stats struct {
		reads        atomic.Int64
		writes       atomic.Int64
		readErrors   atomic.Int64
		writeErrors  atomic.Int64
	}

	// Simulate production load
	stopLoad := make(chan struct{})
	var loadWg sync.WaitGroup

	// Multiple readers
	for i := 0; i < 20; i++ {
		loadWg.Add(1)
		go func(id int) {
			defer loadWg.Done()
			for {
				select {
				case <-stopLoad:
					return
				default:
					recordID := fmt.Sprintf("record-%d", id*100%5000)
					_, err := store.GetRecord(ctx, recordID)
					if err != nil {
						stats.readErrors.Add(1)
					} else {
						stats.reads.Add(1)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Multiple writers
	for i := 0; i < 10; i++ {
		loadWg.Add(1)
		go func(id int) {
			defer loadWg.Done()
			writeCount := 0
			for {
				select {
				case <-stopLoad:
					return
				default:
					recordID := fmt.Sprintf("load-writer-%d-%d", id, writeCount)
					record := &pb.CollectionRecord{
						Id:  recordID,
						Metadata: &pb.Metadata{
							Labels:    map[string]string{},
							CreatedAt: timestamppb.Now(),
							UpdatedAt: timestamppb.Now(),
						},
						ProtoData: []byte(fmt.Sprintf("load-data-%d", writeCount)),
					}
					err := store.CreateRecord(ctx, record)
					if err != nil {
						stats.writeErrors.Add(1)
					} else {
						stats.writes.Add(1)
						writeCount++
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	// Let load stabilize
	time.Sleep(100 * time.Millisecond)

	// Capture stats before backup
	readsBefore := stats.reads.Load()
	writesBefore := stats.writes.Load()

	// Perform backup under load
	backupPath := filepath.Join(tmpDir, "backup-under-load.db")
	backupStart := time.Now()

	if err := store.Backup(ctx, backupPath); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	backupDuration := time.Since(backupStart)

	// Continue load briefly after backup
	time.Sleep(100 * time.Millisecond)
	close(stopLoad)
	loadWg.Wait()

	// Capture stats after backup
	readsDuring := stats.reads.Load() - readsBefore
	writesDuring := stats.writes.Load() - writesBefore

	t.Logf("Backup duration: %v", backupDuration)
	t.Logf("Total reads: %d (during backup: %d)", stats.reads.Load(), readsDuring)
	t.Logf("Total writes: %d (during backup: %d)", stats.writes.Load(), writesDuring)
	t.Logf("Read errors: %d", stats.readErrors.Load())
	t.Logf("Write errors: %d", stats.writeErrors.Load())

	if readsDuring == 0 {
		t.Error("No reads completed during backup - reads blocked!")
	}

	if writesDuring == 0 {
		t.Error("No writes completed during backup - writes blocked!")
	}

	if stats.readErrors.Load() > 0 || stats.writeErrors.Load() > 0 {
		t.Errorf("Errors during load: reads=%d writes=%d",
			stats.readErrors.Load(), stats.writeErrors.Load())
	}

	// Verify backup
	backupStore, err := NewSqliteStore(backupPath, collection.Options{})
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	defer backupStore.Close()

	count, _ := backupStore.CountRecords(ctx)
	t.Logf("Backup contains %d records", count)

	if count < 5000 {
		t.Errorf("backup missing records: got %d, want at least 5000", count)
	}
}

// TestBackupOnline tests incremental backup method
func TestBackupOnline(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert test data
	numRecords := 5000
	for i := 0; i < numRecords; i++ {
		record := &pb.CollectionRecord{
			Id:  fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Track concurrent operations
	var opsCompleted atomic.Int64
	var opErrors atomic.Int64
	done := make(chan struct{})

	// Concurrent operations during backup
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				// Mix of reads and writes
				if time.Now().UnixNano()%2 == 0 {
					_, err := store.GetRecord(ctx, fmt.Sprintf("record-%d", 42))
					if err != nil {
						opErrors.Add(1)
					} else {
						opsCompleted.Add(1)
					}
				} else {
					record := &pb.CollectionRecord{
						Id:  fmt.Sprintf("concurrent-%d", time.Now().UnixNano()),
						Metadata: &pb.Metadata{
							Labels:    map[string]string{},
							CreatedAt: timestamppb.Now(),
							UpdatedAt: timestamppb.Now(),
						},
						ProtoData: []byte("concurrent"),
					}
					err := store.CreateRecord(ctx, record)
					if err != nil {
						opErrors.Add(1)
					} else {
						opsCompleted.Add(1)
					}
				}
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Perform online backup (incremental)
	backupPath := filepath.Join(tmpDir, "backup-online.db")
	backupStart := time.Now()

	// Use batch size of 100 pages
	if err := store.BackupOnline(ctx, backupPath, 100); err != nil {
		t.Fatalf("online backup failed: %v", err)
	}

	backupDuration := time.Since(backupStart)
	close(done)
	time.Sleep(10 * time.Millisecond)

	t.Logf("Online backup duration: %v", backupDuration)
	t.Logf("Concurrent operations: %d", opsCompleted.Load())
	t.Logf("Operation errors: %d", opErrors.Load())

	if opsCompleted.Load() == 0 {
		t.Error("No concurrent operations completed - backup blocked everything!")
	}

	if opErrors.Load() > 0 {
		t.Errorf("Errors during concurrent operations: %d", opErrors.Load())
	}

	// Verify backup
	backupStore, err := NewSqliteStore(backupPath, collection.Options{})
	if err != nil {
		t.Fatalf("failed to open backup: %v", err)
	}
	defer backupStore.Close()

	count, err := backupStore.CountRecords(ctx)
	if err != nil {
		t.Fatalf("failed to count records: %v", err)
	}

	if count < int64(numRecords) {
		t.Errorf("backup missing records: got %d, want at least %d", count, numRecords)
	}
}

// TestBackupFailureRecovery tests error handling
func TestBackupFailureRecovery(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Test backup to invalid path
	invalidPath := "/invalid/path/that/does/not/exist/backup.db"
	err = store.Backup(ctx, invalidPath)
	if err == nil {
		t.Error("expected error for invalid backup path, got nil")
	}

	// Verify original database still works after failed backup
	record := &pb.CollectionRecord{
		Id:  "test-after-failure",
		Metadata: &pb.Metadata{
			Labels:    map[string]string{},
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
		},
		ProtoData: []byte("data"),
	}
	if err := store.CreateRecord(ctx, record); err != nil {
		t.Errorf("original database broken after backup failure: %v", err)
	}

	retrieved, err := store.GetRecord(ctx, "test-after-failure")
	if err != nil {
		t.Errorf("failed to read from original after backup failure: %v", err)
	}
	if retrieved == nil || retrieved.Id != "test-after-failure" {
		t.Error("data corrupted after backup failure")
	}
}

// BenchmarkBackupWithLoad measures backup performance under load
func BenchmarkBackupWithLoad(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	store, err := NewSqliteStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		record := &pb.CollectionRecord{
			Id:  fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		store.CreateRecord(ctx, record)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		backupPath := filepath.Join(tmpDir, fmt.Sprintf("backup-%d.db", i))
		if err := store.Backup(ctx, backupPath); err != nil {
			b.Fatalf("backup failed: %v", err)
		}
	}
}
