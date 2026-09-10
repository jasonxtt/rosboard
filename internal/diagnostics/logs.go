package diagnostics

import (
	"bytes"
	"strings"
	"sync"
)

const (
	defaultLogBufferLines = 500
	defaultLogBufferBytes = 64 << 10
)

// LogSource is the deliberately small boundary used by diagnostic export.
// The running service may provide a bounded in-memory copy of its stdout log;
// export never opens arbitrary files or reads the process environment.
type LogSource interface {
	Recent() string
}

// LogBuffer keeps only a small tail of the process log. It is safe to use as
// an io.Writer alongside the existing stdout logger.
type LogBuffer struct {
	mu       sync.RWMutex
	data     []byte
	maxLines int
	maxBytes int
}

func NewLogBuffer(maxLines, maxBytes int) *LogBuffer {
	if maxLines <= 0 {
		maxLines = defaultLogBufferLines
	}
	if maxBytes <= 0 {
		maxBytes = defaultLogBufferBytes
	}
	return &LogBuffer{maxLines: maxLines, maxBytes: maxBytes}
}

func (b *LogBuffer) Write(payload []byte) (int, error) {
	if b == nil {
		return len(payload), nil
	}
	originalLength := len(payload)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(payload) >= b.maxBytes {
		payload = payload[len(payload)-b.maxBytes:]
		b.data = append(b.data[:0], payload...)
		b.trimLocked()
		return originalLength, nil
	}
	if overflow := len(b.data) + len(payload) - b.maxBytes; overflow > 0 {
		b.data = append([]byte(nil), b.data[overflow:]...)
	}
	b.data = append(b.data, payload...)
	b.trimLocked()
	return originalLength, nil
}

func (b *LogBuffer) Recent() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return string(append([]byte(nil), b.data...))
}

func (b *LogBuffer) trimLocked() {
	if len(b.data) > b.maxBytes {
		b.data = append([]byte(nil), b.data[len(b.data)-b.maxBytes:]...)
	}
	if b.maxLines <= 0 {
		return
	}
	newlines := bytes.Count(b.data, []byte{'\n'})
	for newlines > b.maxLines {
		index := bytes.IndexByte(b.data, '\n')
		if index < 0 {
			break
		}
		b.data = append([]byte(nil), b.data[index+1:]...)
		newlines--
	}
	// A split write can leave a large unterminated line. Keep the same byte
	// boundary even when there are no newline separators.
	b.data = []byte(strings.TrimPrefix(string(b.data), "\n"))
}
