package jobfile

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
)

// Job is the fully-decoded view of one JSONL file. Header is always
// present (Read errors otherwise); Progress is the ordered list of
// recorded events; Terminal is nil when the job is still in flight
// or the writer crashed before the closing line.
type Job struct {
	Header   HeaderLine
	Progress []ProgressLine
	Terminal *TerminalLine
}

// Read returns the parsed job log at
// <libRoot>/.kura/jobs/<jobID>.jsonl. Tolerates a malformed or
// truncated last line (returned as if it weren't there) so a crash
// mid-write doesn't corrupt diagnostics. fs.ErrNotExist signals "no
// such job" — callers branch on errors.Is.
func Read(libRoot, jobID string) (Job, error) {
	path := paths.JobFile(libRoot, jobID)
	file, err := os.Open(path)
	if err != nil {
		return Job{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4 MiB max line; results stay well under

	var job Job
	headerSeen := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			// Truncated / malformed final line. Stop reading; return
			// what we have so far.
			break
		}
		switch probe.Type {
		case LineHeader:
			if headerSeen {
				return Job{}, fmt.Errorf("jobfile: duplicate header in %s", path)
			}
			if err := json.Unmarshal(line, &job.Header); err != nil {
				return Job{}, fmt.Errorf("jobfile: parse header: %w", err)
			}
			headerSeen = true
		case LineProgress:
			var p ProgressLine
			if err := json.Unmarshal(line, &p); err != nil {
				// Skip; treat as truncated.
				return job, nil
			}
			job.Progress = append(job.Progress, p)
		case LineTerminal:
			var t TerminalLine
			if err := json.Unmarshal(line, &t); err != nil {
				return job, nil
			}
			job.Terminal = &t
			// Terminal is the last line; further records are unexpected
			// but tolerated (return what we have).
			return job, nil
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return Job{}, fmt.Errorf("jobfile: scan: %w", err)
	}
	if !headerSeen {
		return Job{}, fmt.Errorf("jobfile: %s missing header", path)
	}
	return job, nil
}
