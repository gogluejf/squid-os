package chat

import (
	"fmt"
	"testing"

	"squid-os/internal/config"
)

// generateSyntheticSession builds a large synthetic session with N tool-call
// messages spread across P paths, with a mix of reads, edits, and writes.
// Each path gets a final full read as the latest checkpoint.
func generateSyntheticSession(numMessages, numPaths int) []config.Message {
	messages := make([]config.Message, 0, numMessages)

	for i := 0; i < numMessages; i++ {
		path := "/file" + string(rune('A'+(i%numPaths))) + ".go"
		var tc config.ToolCallEntry

		switch i % 4 {
		case 0:
			// Read
			tc = tcRead(fmt.Sprintf("tc_%d", i), path, true, 50, 100)
		case 1:
			// Edit
			tc = tcEdit(fmt.Sprintf("tc_%d", i), path, false, true, 40, 20)
		case 2:
			// Write
			tc = tcWrite(fmt.Sprintf("tc_%d", i), path, i%10 == 2, true, 60, 30)
		default:
			// Read
			tc = tcRead(fmt.Sprintf("tc_%d", i), path, true, 50, 100)
		}

		messages = append(messages, msgWithTools(fmt.Sprintf("msg_%d", i), []config.ToolCallEntry{tc}))
	}

	// Add a final full read for each path to ensure a clear checkpoint
	for p := 0; p < numPaths; p++ {
		path := "/file" + string(rune('A'+p)) + ".go"
		tc := tcRead(fmt.Sprintf("tc_final_%d", p), path, true, 50, 100)
		messages = append(messages, msgWithTools(fmt.Sprintf("msg_final_%d", p), []config.ToolCallEntry{tc}))
	}

	return messages
}

// BenchmarkBuildCompactionPlan_small benchmarks compaction on a small session
// (100 messages, 5 paths).
func BenchmarkBuildCompactionPlan_small(b *testing.B) {
	messages := generateSyntheticSession(100, 5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildCompactionPlan(messages)
	}
}

// BenchmarkBuildCompactionPlan_medium benchmarks compaction on a medium session
// (500 messages, 20 paths).
func BenchmarkBuildCompactionPlan_medium(b *testing.B) {
	messages := generateSyntheticSession(500, 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildCompactionPlan(messages)
	}
}

// BenchmarkBuildCompactionPlan_large benchmarks compaction on a large session
// (2000 messages, 50 paths).
func BenchmarkBuildCompactionPlan_large(b *testing.B) {
	messages := generateSyntheticSession(2000, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildCompactionPlan(messages)
	}
}

// BenchmarkBuildCompactionPlan_xlarge benchmarks compaction on an extra-large session
// (10000 messages, 100 paths).
func BenchmarkBuildCompactionPlan_xlarge(b *testing.B) {
	messages := generateSyntheticSession(10000, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildCompactionPlan(messages)
	}
}
