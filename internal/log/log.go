package log

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"squid-os/internal/config"
)

var (
	mu                 sync.Mutex
	enabled            bool
	voiceEpoch         uint64
	sseLogger          *log.Logger
	voiceLogger        *log.Logger
	metricsLogger      *log.Logger
	gitShortStatLogger *log.Logger
	pasteLogger        *log.Logger
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
	gitShortStatPath := paths.Logs + "/git_shortstat.log"
	pastePath := paths.Logs + "/paste_debug.log"
	voicePath := paths.Logs + "/voice.log"

	// Cleanup massive logs on boot (if > 100MB)
	truncateIfOverLimit(ssePath)
	truncateIfOverLimit(metricsPath)
	truncateIfOverLimit(gitShortStatPath)
	truncateIfOverLimit(pastePath)
	truncateIfOverLimit(voicePath)

	sseF, err := os.OpenFile(ssePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		sseLogger = log.New(sseF, "", 0)
	}

	metricsF, err := os.OpenFile(metricsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		metricsLogger = log.New(metricsF, "", 0)
	}

	gitShortStatF, err := os.OpenFile(gitShortStatPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		gitShortStatLogger = log.New(gitShortStatF, "", 0)
	}

	pasteF, err := os.OpenFile(pastePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		pasteLogger = log.New(pasteF, "", 0)
	}

	voiceF, err := os.OpenFile(voicePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		voiceLogger = log.New(voiceF, "", 0)
	}
}

// SetEnabled enables or disables logging at runtime.
func SetEnabled(v bool) {
	mu.Lock()
	defer mu.Unlock()
	if !v {
		voiceEpoch++
	}
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

// LogGitShortStat writes a timestamped git shortstat lifecycle event to
// git_shortstat.log. event is one of: "sync-first", "bg-start", "bg-done".
// Only git-triggering events are logged (cache hits are intentionally skipped).
func LogGitShortStat(event, dir string, age, dur time.Duration) {
	if !IsEnabled() {
		return
	}
	if gitShortStatLogger == nil {
		return
	}
	gitShortStatLogger.Printf("%s %s dir=%s age=%s dur=%s\n",
		time.Now().Format("15:04:05.000000"), event, dir,
		age.Round(time.Millisecond), dur.Round(time.Millisecond))
}

// LogPaste writes a timestamped paste/clipboard event to paste_debug.log.
// Text is truncated to 80 characters to keep the log small.
func LogPaste(label, text string, err error) {
	if !IsEnabled() {
		return
	}
	if pasteLogger == nil {
		return
	}
	errStr := "<nil>"
	if err != nil {
		errStr = err.Error()
	}
	shortText := text
	if len(shortText) > 80 {
		shortText = shortText[:80] + "..."
	}
	pasteLogger.Printf("%s %s: text=%q err=%s\n",
		time.Now().Format("15:04:05.000000"), label, shortText, errStr)
}

// LogVoice writes to the dedicated voice.log debug sink. A bounded
// queue keeps disk IO off the UI/playback locks; overload drops diagnostics.
// Epoch checks discard queued records across privacy/debug disable transitions.
var voiceQueue = make(chan voiceRecord, 128)
var voiceWorker sync.Once

type voiceRecord struct {
	line  string
	epoch uint64
}

func LogVoice(event, format string, args ...any) {
	mu.Lock()
	if !enabled || voiceLogger == nil {
		mu.Unlock()
		return
	}
	epoch := voiceEpoch
	mu.Unlock()
	r := voiceRecord{fmt.Sprintf("%s voice.%s %s", time.Now().Format("15:04:05.000000"), event, fmt.Sprintf(format, args...)), epoch}
	voiceWorker.Do(func() {
		go func() {
			for r := range voiceQueue {
				mu.Lock()
				logger := voiceLogger
				allowed := enabled && r.epoch == voiceEpoch
				mu.Unlock()
				if allowed && logger != nil {
					logger.Print(r.line)
				}
			}
		}()
	})
	select {
	case voiceQueue <- r:
	default:
	}
}
