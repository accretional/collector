package collection

import (
	"context"
	"fmt"
	"log"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/logging"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SystemLogger implements logging.Logger and writes to a collection
type SystemLogger struct {
	collection *Collection
	fields     map[string]string
	buffer     []*pb.SystemLog
	mu         sync.Mutex
	workerRun  bool
	workerChan chan *pb.SystemLog
}

// NewSystemLogger creates a new system logger
func NewSystemLogger(coll *Collection) *SystemLogger {
	l := &SystemLogger{
		collection: coll,
		fields:     make(map[string]string),
		workerChan: make(chan *pb.SystemLog, 1000), // Buffer 1000 logs
		workerRun:  true,
	}
	go l.worker()
	return l
}

func (l *SystemLogger) Close() {
	l.workerRun = false
	close(l.workerChan)
}

func (l *SystemLogger) worker() {
	for logEntry := range l.workerChan {
		l.writeLog(logEntry)
	}
}

func (l *SystemLogger) writeLog(logEntry *pb.SystemLog) {
	if l.collection == nil {
		return
	}

	protoData, err := proto.Marshal(logEntry)
	if err != nil {
		fmt.Printf("Error marshaling log: %v\n", err)
		return
	}

	record := &pb.CollectionRecord{
		Id:        logEntry.Id,
		ProtoData: protoData,
		Metadata: &pb.Metadata{
			CreatedAt: logEntry.Timestamp,
			Labels: map[string]string{
				"level":     logEntry.Level.String(),
				"component": logEntry.Component,
			},
		},
	}

	// We ignore errors here to prevent infinite recursion if logging fails
	_ = l.collection.CreateRecord(context.Background(), record)
}

func (l *SystemLogger) log(level logging.Level, msg string, args ...any) {
	entry := &pb.SystemLog{
		Id:        uuid.New().String(),
		Timestamp: timestamppb.Now(),
		Level:     pb.SystemLog_Level(level), // Assuming 1:1 mapping for now or map explicitly
		Message:   msg,
		Fields:    l.fields,
	}

	// Map level (proto starts at 0=DEBUG, our pkg starts at 0=DEBUG)
	// Just to be safe:
	switch level {
	case logging.LevelDebug:
		entry.Level = pb.SystemLog_DEBUG
	case logging.LevelInfo:
		entry.Level = pb.SystemLog_INFO
	case logging.LevelWarn:
		entry.Level = pb.SystemLog_WARNING
	case logging.LevelError:
		entry.Level = pb.SystemLog_ERROR
	}

	// Merge fields from args
	if len(args) > 0 {
		// Clone existing fields
		newFields := make(map[string]string)
		for k, v := range l.fields {
			newFields[k] = v
		}

		for i := 0; i < len(args); i += 2 {
			if i+1 < len(args) {
				key := fmt.Sprintf("%v", args[i])
				val := fmt.Sprintf("%v", args[i+1])
				newFields[key] = val
			}
		}
		entry.Fields = newFields
	}

	// Queue it
	select {
	case l.workerChan <- entry:
	default:
		// Drop log if buffer full to prevent blocking
		fmt.Println("System log buffer full, dropping log")
	}

	// Also print to standard log for visibility
	log.Printf("[%s] %s %v", level.String(), msg, args)
}

func (l *SystemLogger) Debug(msg string, args ...any) { l.log(logging.LevelDebug, msg, args...) }
func (l *SystemLogger) Info(msg string, args ...any)  { l.log(logging.LevelInfo, msg, args...) }
func (l *SystemLogger) Warn(msg string, args ...any)  { l.log(logging.LevelWarn, msg, args...) }
func (l *SystemLogger) Error(msg string, args ...any) { l.log(logging.LevelError, msg, args...) }

func (l *SystemLogger) With(args ...any) logging.Logger {
	newLogger := &SystemLogger{
		collection: l.collection,
		fields:     make(map[string]string),
		workerChan: l.workerChan, // Share the channel
		workerRun:  l.workerRun,
	}

	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}

	// Add new fields
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key := fmt.Sprintf("%v", args[i])
			val := fmt.Sprintf("%v", args[i+1])
			newLogger.fields[key] = val
		}
	}

	return newLogger
}
