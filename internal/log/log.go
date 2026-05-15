package log

import (
	"log"
	"os"
	"time"

	"squid-os/internal/config"
)

var (
	sseLogger     *log.Logger
	metricsLogger *log.Logger
)

// Init opens the SSE and stream metrics log files at the given paths.
// Call once early in startup after paths.EnsureDirs().
func Init(paths config.Paths) {
	ssePath := paths.Logs + "/sse_chunks.log"
	metricsPath := paths.Logs + "/stream_metrics.log"

	sseF, err := os.OpenFile(ssePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		sseLogger = log.New(sseF, "", 0)
	}

	metricsF, err := os.OpenFile(metricsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		metricsLogger = log.New(metricsF, "", 0)
	}
}

// LogSSEChunk writes a timestamped SSE line to sse_chunks.log.
func LogSSEChunk(line string) {
	if sseLogger == nil {
		return
	}
	sseLogger.Printf("%s %s\n", time.Now().Format("15:04:05.000000"), line)
}

// LogStreamMetrics writes a timestamped metric event to stream_metrics.log.
// kind is one of: "addTextChars", "addThinkChars", "addToolCallChars"
func LogStreamMetrics(kind, chunk string, n, total int, first, done time.Time) {
	if metricsLogger == nil {
		return
	}
	metricsLogger.Printf("%s %s n=%d chars=%d chunk=%q first=%s done=%s\n",
		time.Now().Format("15:04:05.000000"), kind, n, total, chunk,
		first.Format("15:04:05.000000"), done.Format("15:04:05.000000"))
}
