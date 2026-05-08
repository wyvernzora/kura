package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// JobEvent is one event emitted by the SSE stream. Exactly one of
// Progress / Result / Error is set depending on the event kind.
type JobEvent struct {
	Kind     string          // "progress" | "result" | "error"
	Progress *JobProgress    // when Kind == "progress"
	Result   json.RawMessage // when Kind == "result"; raw so callers decode into typed result
	Error    *JobError       // when Kind == "error"
}

// JobProgress mirrors the rest server's wire shape.
type JobProgress struct {
	Phase   string `json:"phase"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

// JobError mirrors the rest server's wire shape.
type JobError struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// StreamJob opens an SSE connection to /api/v1/jobs/{job}/stream and
// invokes onEvent for each event until the terminal result/error
// arrives or ctx cancels. Returns nil after a clean terminal event;
// returns the wrapped server error for terminal failures.
func (c *Client) StreamJob(ctx context.Context, jobID string, onEvent func(JobEvent)) error {
	req, err := buildJobStreamRequest(ctx, c, jobID)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return discoveryHint(err, c.BaseURL)
	}
	defer resp.Body.Close()
	if err := checkJobStreamStatus(resp); err != nil {
		return err
	}

	reader := bufio.NewReader(resp.Body)
	var (
		eventName string
		dataBuf   strings.Builder
	)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		done, err := handleSSELine(strings.TrimRight(line, "\r\n"), &eventName, &dataBuf, onEvent)
		if done {
			return err
		}
	}
}

// buildJobStreamRequest constructs the SSE upgrade request, including
// the Accept header and the operator/bearer auth headers the rest of
// the client speaks. The bearer is required: the SSE handler is
// hand-rolled (vs. Do's JSON path) and silently dropping the header
// leads to 401s on every async-job follow-up.
func buildJobStreamRequest(ctx context.Context, c *Client, jobID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/jobs/"+jobID+"/stream", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.Operator {
		req.Header.Set(headerOperator, "1")
	}
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	return req, nil
}

// checkJobStreamStatus inspects the SSE response status. 200 returns
// nil; non-200 attempts to decode the body as an ErrorEnvelope, with
// a generic fallback for non-JSON error bodies.
func checkJobStreamStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var env ErrorEnvelope
	if jerr := json.NewDecoder(resp.Body).Decode(&env); jerr == nil {
		env.Status = resp.StatusCode
		return &env
	}
	return fmt.Errorf("stream returned %d %s", resp.StatusCode, resp.Status)
}

// handleSSELine consumes one trimmed SSE line. The blank-line
// separator dispatches the accumulated event via onEvent; "event:"
// and "data:" prefixes update the per-event accumulators; comments
// and unknown fields are ignored. Returns done=true with err set to
// the terminal error (or nil) when the dispatched event is a
// "result" or "error" — the caller breaks out of the read loop.
func handleSSELine(
	line string,
	eventName *string,
	dataBuf *strings.Builder,
	onEvent func(JobEvent),
) (done bool, err error) {
	if line == "" {
		if *eventName == "" {
			return false, nil
		}
		ev := decodeEvent(*eventName, dataBuf.String())
		onEvent(ev)
		*eventName = ""
		dataBuf.Reset()
		if ev.Kind == "result" || ev.Kind == "error" {
			return true, finalErrorFor(ev)
		}
		return false, nil
	}
	if rest, ok := strings.CutPrefix(line, "event:"); ok {
		*eventName = strings.TrimSpace(rest)
		return false, nil
	}
	if rest, ok := strings.CutPrefix(line, "data:"); ok {
		if dataBuf.Len() > 0 {
			dataBuf.WriteByte('\n')
		}
		dataBuf.WriteString(strings.TrimSpace(rest))
	}
	// Comments (":") and unknown fields ignored.
	return false, nil
}

func decodeEvent(name, data string) JobEvent {
	switch name {
	case "progress":
		var p JobProgress
		_ = json.Unmarshal([]byte(data), &p)
		return JobEvent{Kind: "progress", Progress: &p}
	case "result":
		raw := json.RawMessage(data)
		return JobEvent{Kind: "result", Result: raw}
	case "error":
		var e JobError
		_ = json.Unmarshal([]byte(data), &e)
		return JobEvent{Kind: "error", Error: &e}
	}
	return JobEvent{Kind: name}
}

func finalErrorFor(ev JobEvent) error {
	if ev.Kind == "error" && ev.Error != nil {
		return &ErrorEnvelope{
			Kind:    ev.Error.Kind,
			Message: ev.Error.Message,
			Data:    ev.Error.Data,
		}
	}
	return nil
}

// PollJob polls GET /api/v1/jobs/{id} until the job reaches a
// terminal state or ctx cancels. Calls onProgress for each new
// progress snapshot. Returns the raw result JSON on success or a
// decoded ErrorEnvelope on failure.
//
// Polling fallback for environments where SSE is awkward (some
// proxies buffer streams). CLI default path is StreamJob.
func (c *Client) PollJob(ctx context.Context, jobID string, interval time.Duration, onProgress func(JobProgress)) (json.RawMessage, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	var lastProgress *JobProgress
	for {
		var status struct {
			State    string          `json:"state"`
			Progress *JobProgress    `json:"progress,omitempty"`
			Result   json.RawMessage `json:"result,omitempty"`
			Error    *JobError       `json:"error,omitempty"`
		}
		if err := c.Do(ctx, http.MethodGet, "/api/v1/jobs/"+jobID, nil, nil, &status, false); err != nil {
			return nil, err
		}
		if status.Progress != nil && (lastProgress == nil || *lastProgress != *status.Progress) && onProgress != nil {
			onProgress(*status.Progress)
			cp := *status.Progress
			lastProgress = &cp
		}
		switch status.State {
		case "succeeded":
			return status.Result, nil
		case "failed":
			if status.Error != nil {
				return nil, &ErrorEnvelope{Kind: status.Error.Kind, Message: status.Error.Message, Data: status.Error.Data}
			}
			return nil, fmt.Errorf("job %s failed (no error envelope)", jobID)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}
