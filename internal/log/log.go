package log

import (
	"log"
	"os"
	"sync"
	"time"

	"squid-os/internal/config"
)

var (
	mu            sync.Mutex
	enabled       bool
	sseLogger     *log.Logger
	metricsLogger *log.Logger
)

// maxLogSize is the threshold (in bytes) for truncating log files on boot.
const maxLogSize = 50 * 1024 * 1024 // 100 MB

// truncateIfOverLimit wipes a file to zero if it exceeds maxLogSize.
func truncateIfOverLimit(path string) {
	info, err := os.Stat(path)
	if err == nil && info.Size() > maxLogSize {
		os.Truncate(path, 0)
	}
}

// Init opens the SSE and stream metrics log files at the given paths.
// Truncates logs on boot if they exceed the size limit (100MB).
// Call once early in startup after paths.EnsureDirs().
func Init(paths config.Paths) {
	ssePath := paths.Logs + "/sse_chunks.log"
	metricsPath := paths.Logs + "/stream_metrics.log"

	// Cleanup massive logs on boot (if > 100MB)
	truncateIfOverLimit(ssePath)
	truncateIfOverLimit(metricsPath)

	sseF, err := os.OpenFile(ssePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		sseLogger = log.New(sseF, "", 0)
	}

	metricsF, err := os.OpenFile(metricsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		metricsLogger = log.New(metricsF, "", 0)
	}
}

// SetEnabled enables or disables logging at runtime.
func SetEnabled(v bool) {
	mu.Lock()
	defer mu.Unlock()
	enabled = v
}

// IsEnabled returns true if logging is currently active.
func IsEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// LogSSEChunk writes a timestamped SSE line to sse_chunks.log.
func LogSSEChunk(line string) {
	if !IsEnabled() {
		return
	}
	if sseLogger == nil {
		return
	}
	sseLogger.Printf("%s %s\n", time.Now().Format("15:04:05.000000"), line)
}

// LogStreamMetrics writes a timestamped metric event to stream_metrics.log.
// kind is one of: "addTextChars", "addThinkChars", "addToolCallChars"
func LogStreamMetrics(kind, chunk string, n, total int, first, done time.Time) {
	if !IsEnabled() {
		return
	}
	if metricsLogger == nil {
		return
	}
	metricsLogger.Printf("%s %s n=%d chars=%d chunk=%q first=%s done=%s\n",
		time.Now().Format("15:04:05.000000"), kind, n, total, chunk,
		first.Format("15:04:05.000000"), done.Format("15:04:05.000000"))
}
