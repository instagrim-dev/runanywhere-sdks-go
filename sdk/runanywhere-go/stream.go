package runanywhere

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// ChatStreamReader reads Server-Sent Events from a chat completion stream.
// Call Next() until io.EOF or a non-nil error; [DONE] is signaled by io.EOF after the last chunk.
type ChatStreamReader struct {
	body   io.ReadCloser
	scan   *bufio.Scanner
	err    error
	closed bool
}

// Max size for a single SSE line (e.g. "data: {...}"). bufio.Scanner defaults to 64KiB.
const sseMaxTokenSize = 1024 * 1024 // 1 MiB

// NewChatStreamReader creates a reader that parses SSE from the given response body.
// The caller must close the returned reader when done.
func NewChatStreamReader(body io.ReadCloser) *ChatStreamReader {
	scan := bufio.NewScanner(body)
	scan.Buffer(make([]byte, 0, 4096), sseMaxTokenSize)
	return &ChatStreamReader{
		body: body,
		scan: scan,
	}
}

// Next returns the next stream chunk, or io.EOF when the stream ends (including after [DONE]).
// The scanner splits on newlines; we accumulate lines until we see a blank line (end of SSE event).
func (r *ChatStreamReader) Next() (*StreamChunk, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.closed {
		return nil, io.EOF
	}

	var dataLines []string
	for r.scan.Scan() {
		line := r.scan.Text()
		if strings.TrimSpace(line) == "" {
			// End of one SSE event (blank line; accept CRLF/LF/CR)
			if len(dataLines) == 0 {
				continue
			}
			dataLine := strings.Join(dataLines, "\n")
			dataLines = dataLines[:0]
			chunk, err := r.parseEvent(dataLine)
			if err != nil {
				r.err = err
				return nil, err
			}
			if chunk == nil {
				// [DONE]
				_ = r.Close()
				return nil, io.EOF
			}
			return chunk, nil
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			payload := after
			payload = strings.TrimPrefix(payload, " ") // accept "data: " or "data:"
			dataLines = append(dataLines, payload)
		}
	}
	if err := r.scan.Err(); err != nil {
		r.err = err
		return nil, err
	}
	// Flush final event when stream ends without trailing blank line
	if len(dataLines) > 0 {
		dataLine := strings.Join(dataLines, "\n")
		chunk, err := r.parseEvent(dataLine)
		if err != nil {
			r.err = err
			return nil, err
		}
		if chunk != nil {
			_ = r.Close()
			return chunk, nil
		}
	}
	_ = r.Close()
	return nil, io.EOF
}

func (r *ChatStreamReader) parseEvent(data string) (*StreamChunk, error) {
	data = strings.TrimSpace(data)
	if data == "[DONE]" {
		return nil, nil
	}
	var chunk StreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, err
	}
	return &chunk, nil
}

// Close closes the underlying response body.
func (r *ChatStreamReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.body.Close()
}
