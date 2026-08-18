package media

import (
	"fmt"
)

// SessionQuota tracks accumulated resource usage across all attachments
// in a session.
type SessionQuota struct {
	TotalBytes int64
	FileCount  int
	Limits     Limits
}

// NewSessionQuota creates a quota tracker with the given limits.
func NewSessionQuota(limits Limits) *SessionQuota {
	if limits.MaxFiles == 0 {
		limits.MaxFiles = DefaultLimits.MaxFiles
	}
	if limits.MaxSizeBytes == 0 {
		limits.MaxSizeBytes = DefaultLimits.MaxSizeBytes
	}
	return &SessionQuota{Limits: limits}
}

// CheckBeforeIngest verifies that adding a file of the given size
// would not exceed session limits. Returns an error if the limits would be breached.
func (q *SessionQuota) CheckBeforeIngest(size int64) error {
	if q.FileCount >= q.Limits.MaxFiles {
		return fmt.Errorf("session file limit reached (%d files)", q.Limits.MaxFiles)
	}
	if q.TotalBytes+size > q.Limits.MaxSizeBytes {
		return fmt.Errorf("session size limit would be exceeded: %d + %d > %d",
			q.TotalBytes, size, q.Limits.MaxSizeBytes)
	}
	return nil
}

// RecordIngest updates the quota after a successful ingestion.
func (q *SessionQuota) RecordIngest(size int64) {
	q.TotalBytes += size
	q.FileCount++
}

// Remaining returns the remaining capacity in bytes and file slots.
func (q *SessionQuota) Remaining() (bytesRemaining int64, filesRemaining int) {
	bytesRemaining = q.Limits.MaxSizeBytes - q.TotalBytes
	if bytesRemaining < 0 {
		bytesRemaining = 0
	}
	filesRemaining = q.Limits.MaxFiles - q.FileCount
	if filesRemaining < 0 {
		filesRemaining = 0
	}
	return
}

// IsExhausted returns true if no more files can be ingested.
func (q *SessionQuota) IsExhausted() bool {
	return q.FileCount >= q.Limits.MaxFiles || q.TotalBytes >= q.Limits.MaxSizeBytes
}
