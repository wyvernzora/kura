package backupplan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

var sessionEventFieldsByType = map[EventType][]string{
	EventWaitingForTape:     {"candidates"},
	EventTapeLoaded:         {"tapeId", "volumeId"},
	EventTapeRejected:       {"tapeId", "reason", "detail"},
	EventPlanStarted:        {"planId"},
	EventPlanCompleted:      {"planId", "itemsWritten", "itemsFailed"},
	EventPlanFailed:         {"planId", "reason", "detail"},
	EventPlanCancelled:      {"planId", "reason", "detail"},
	EventEncryptionVerified: {"planId"},
	EventSnapshotSwept:      {"entry", "reason"},
	EventOutboxOverflow:     {},
	EventDivergenceChecked:  {"planId", "result", "extraSnapshots"},
	EventFreshnessChecked:   {"planId", "dropped"},
	EventItemStarted:        {"planId", "metadataRef", "generation", "bytes", "files"},
	EventItemCompleted:      {"planId", "metadataRef", "generation", "snapshot"},
	EventItemFailed:         {"planId", "metadataRef", "generation", "reason", "detail"},
	EventItemSkipped:        {"planId", "metadataRef", "generation", "reason", "detail"},
	EventFileStarted:        {"planId", "metadataRef", "bytes", "path"},
	EventFileCompleted:      {"planId", "metadataRef", "path"},
	EventCancelRequested:    {"planId"},
	EventTerminal:           {"state", "reason", "plansCompleted"},
}

var zeroValueEventFields = []struct {
	name  string
	value json.RawMessage
}{
	{name: "planId", value: json.RawMessage(`""`)},
	{name: "candidates", value: json.RawMessage(`[]`)},
	{name: "tapeId", value: json.RawMessage(`""`)},
	{name: "volumeId", value: json.RawMessage(`""`)},
	{name: "result", value: json.RawMessage(`""`)},
	{name: "extraSnapshots", value: json.RawMessage(`[]`)},
	{name: "dropped", value: json.RawMessage(`[]`)},
	{name: "metadataRef", value: json.RawMessage(`""`)},
	{name: "generation", value: json.RawMessage(`0`)},
	{name: "bytes", value: json.RawMessage(`0`)},
	{name: "files", value: json.RawMessage(`0`)},
	{name: "path", value: json.RawMessage(`""`)},
	{name: "entry", value: json.RawMessage(`""`)},
	{name: "snapshot", value: json.RawMessage(`""`)},
	{name: "itemsWritten", value: json.RawMessage(`0`)},
	{name: "itemsFailed", value: json.RawMessage(`0`)},
	{name: "reason", value: json.RawMessage(`""`)},
	{name: "detail", value: json.RawMessage(`""`)},
	{name: "state", value: json.RawMessage(`""`)},
	{name: "plansCompleted", value: json.RawMessage(`0`)},
}

func TestSessionRoundTripAndWireContract(t *testing.T) {
	root := t.TempDir()
	header := validHeader()
	writer, err := CreateSession(root, header)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	events := allDurableEvents()
	for _, event := range events {
		if err := writer.Append(event); err != nil {
			t.Fatalf("Append %s: %v", event.Type, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	session, err := ReadSession(root, header.SessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !reflect.DeepEqual(session.Header, header) {
		t.Fatalf("Header = %#v, want %#v", session.Header, header)
	}
	if got, want := len(session.Events), len(events)-1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if !reflect.DeepEqual(session.Events, events[:len(events)-1]) {
		t.Fatalf("Events = %#v, want %#v", session.Events, events[:len(events)-1])
	}
	if session.Terminal == nil || !reflect.DeepEqual(*session.Terminal, events[len(events)-1]) {
		t.Fatalf("Terminal = %#v, want %#v", session.Terminal, events[len(events)-1])
	}

	lines := readSessionLines(t, root, header.SessionID)
	if got, want := len(lines), len(events)+1; got != want {
		t.Fatalf("line count = %d, want %d", got, want)
	}
	for i, line := range lines {
		var wire map[string]any
		if err := json.Unmarshal(line, &wire); err != nil {
			t.Fatalf("Unmarshal line %d: %v", i+1, err)
		}
		if wire["type"] == nil {
			t.Fatalf("line %d has no type: %s", i+1, line)
		}
		_, hasSchema := wire["schemaVersion"]
		if i == 0 {
			if !hasSchema || wire["schemaVersion"] != float64(1) {
				t.Fatalf("header schemaVersion = %#v, want 1", wire["schemaVersion"])
			}
		} else if hasSchema {
			t.Fatalf("event line %d has schemaVersion: %s", i+1, line)
		}
	}
}

func TestSessionEventWireFieldsByType(t *testing.T) {
	for _, event := range allDurableEvents() {
		t.Run(string(event.Type), func(t *testing.T) {
			data, err := json.Marshal(toEventWire(event))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			wantFields := append(
				[]string{"type", "at", "seq"},
				sessionEventFieldsByType[event.Type]...,
			)
			if len(fields) != len(wantFields) {
				t.Fatalf("wire fields = %v, want %v", fields, wantFields)
			}
			for _, field := range wantFields {
				if _, exists := fields[field]; !exists {
					t.Fatalf("wire is missing field %q: %s", field, data)
				}
			}
		})
	}
}

func TestSessionSequenceReplayAppendsNothing(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	first := Event{
		Type:       EventWaitingForTape,
		At:         testEventTime(1),
		Seq:        1,
		Candidates: []tape.ID{"ABC123L6"},
	}
	if err := writer.Append(first); err != nil {
		t.Fatalf("Append first: %v", err)
	}
	replay := first
	replay.Candidates = []tape.ID{"DEF456L6"}
	if err := writer.Append(replay); err != nil {
		t.Fatalf("Append replay: %v", err)
	}
	older := first
	older.Seq = 0
	err = writer.Append(older)
	assertExactError(t, err, "backupplan: event seq must be greater than zero")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readSessionLines(t, root, testSessionID)
	if got, want := len(lines), 2; got != want {
		t.Fatalf("line count after replay = %d, want %d", got, want)
	}
	if bytes.Contains(lines[1], []byte("DEF456L6")) {
		t.Fatalf("durable event contains replay payload: %s", lines[1])
	}
}

func TestSessionResumeDeduplicatesOverlappingReplay(t *testing.T) {
	root := t.TempDir()
	header := validHeader()
	writer, err := CreateSession(root, header)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	events := allDurableEvents()
	for _, event := range events[:4] {
		if err := writer.Append(event); err != nil {
			t.Fatalf("Append %s: %v", event.Type, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close before restart: %v", err)
	}

	if _, err := CreateSession(root, header); !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateSession after restart error = %v, want os.ErrExist", err)
	}
	writer, err = OpenSession(root, header.SessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	for _, event := range events[2:5] {
		if err := writer.Append(event); err != nil {
			t.Fatalf("Append replay %s: %v", event.Type, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}

	lines := readSessionLines(t, root, header.SessionID)
	if got, want := len(lines), 6; got != want {
		t.Fatalf("line count after overlapping replay = %d, want %d", got, want)
	}
	headerLines := 0
	for _, line := range lines {
		var probe sessionLineProbe
		if err := json.Unmarshal(line, &probe); err != nil {
			t.Fatalf("Unmarshal line: %v", err)
		}
		if probe.Type == "header" {
			headerLines++
		}
	}
	if headerLines != 1 {
		t.Fatalf("header line count = %d, want 1", headerLines)
	}
	session, err := ReadSession(root, header.SessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !reflect.DeepEqual(session.Events, events[:5]) {
		t.Fatalf("Events = %#v, want %#v", session.Events, events[:5])
	}
}

func TestSessionResumeDropsTruncatedFinalLine(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	first := allDurableEvents()[0]
	if err := writer.Append(first); err != nil {
		t.Fatalf("Append first: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	file, err := os.OpenFile(
		sessionFile(root, testSessionID),
		os.O_APPEND|os.O_WRONLY,
		0o664,
	)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString(`{"type":"tape_loaded","seq":2`); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close truncated file: %v", err)
	}

	writer, err = OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	second := allDurableEvents()[1]
	if err := writer.Append(second); err != nil {
		t.Fatalf("Append second: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}
	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !reflect.DeepEqual(session.Events, []Event{first, second}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{first, second})
	}
}

func TestSessionResumeReplaysEventWhoseTrailingNewlineWasLost(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	events := allDurableEvents()[:7]
	for _, event := range events[:6] {
		if err := writer.Append(event); err != nil {
			t.Fatalf("Append %s: %v", event.Type, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	removeSessionTrailingNewline(t, root)

	writer, err = OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	for _, event := range events[3:] {
		if err := writer.Append(event); err != nil {
			t.Fatalf("Append replay %s: %v", event.Type, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}

	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !reflect.DeepEqual(session.Events, events) {
		t.Fatalf("Events = %#v, want %#v", session.Events, events)
	}
}

func TestSessionResumeReplaysTerminalWhoseTrailingNewlineWasLost(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	first := validEvent(EventPlanStarted)
	terminal := validEvent(EventTerminal)
	terminal.Seq = 2
	terminal.At = testEventTime(2)
	if err := writer.Append(first); err != nil {
		t.Fatalf("Append first: %v", err)
	}
	if err := writer.Append(terminal); err != nil {
		t.Fatalf("Append terminal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	removeSessionTrailingNewline(t, root)

	writer, err = OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := writer.Append(terminal); err != nil {
		t.Fatalf("Append replayed terminal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}

	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal == nil || !reflect.DeepEqual(*session.Terminal, terminal) {
		t.Fatalf("Terminal = %#v, want %#v", session.Terminal, terminal)
	}
}

func TestSessionResumeEveryTruncationOffsetPreservesSequence(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	events := allDurableEvents()[:7]
	for _, event := range events[:6] {
		if err := writer.Append(event); err != nil {
			t.Fatalf("Append %s: %v", event.Type, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := sessionFile(root, testSessionID)
	healthy, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile healthy session: %v", err)
	}
	headerEnd := bytes.IndexByte(healthy, '\n') + 1
	if headerEnd == 0 {
		t.Fatal("healthy session has no complete header")
	}

	resumableOffsets := 0
	for offset := 0; offset <= len(healthy); offset++ {
		if err := os.WriteFile(path, healthy[:offset], 0o664); err != nil {
			t.Fatalf("offset %d: WriteFile: %v", offset, err)
		}
		writer, err = OpenSession(root, testSessionID)
		if offset < headerEnd {
			if err == nil {
				_ = writer.Close()
				t.Fatalf("offset %d: OpenSession succeeded without a complete header", offset)
			}
			continue
		}
		resumableOffsets++
		if err != nil {
			t.Fatalf("offset %d: OpenSession: %v", offset, err)
		}
		for _, event := range events {
			if err := writer.Append(event); err != nil {
				t.Fatalf("offset %d: Append seq %d: %v", offset, event.Seq, err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("offset %d: Close: %v", offset, err)
		}
		session, err := ReadSession(root, testSessionID)
		if err != nil {
			t.Fatalf("offset %d: ReadSession: %v", offset, err)
		}
		if !reflect.DeepEqual(session.Events, events) {
			t.Fatalf("offset %d: seqs = %v, want %v", offset, eventSeqs(session), eventSeqs(
				Session{Events: events},
			))
		}
	}
	t.Logf(
		"tested %d total byte offsets; %d had a complete resumable header",
		len(healthy)+1,
		resumableOffsets,
	)
}

func TestSessionResumeRecoversTerminalState(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	terminal := allDurableEvents()[len(allDurableEvents())-1]
	if err := writer.Append(terminal); err != nil {
		t.Fatalf("Append terminal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	writer, err = OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	err = writer.Append(Event{
		Type:   EventPlanStarted,
		At:     testEventTime(19),
		Seq:    terminal.Seq + 1,
		PlanID: testPlanID,
	})
	assertExactError(t, err, "backupplan: append after terminal event")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}
}

func TestSessionResumeDeduplicatesReplayAfterTerminal(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	terminal := validEvent(EventTerminal)
	if err := writer.Append(terminal); err != nil {
		t.Fatalf("Append terminal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close created writer: %v", err)
	}

	writer, err = OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := writer.Append(terminal); err != nil {
		t.Fatalf("Append replayed terminal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}
	lines := readSessionLines(t, root, testSessionID)
	if got, want := len(lines), 2; got != want {
		t.Fatalf("line count after terminal replay = %d, want %d", got, want)
	}
}

func TestSessionRejectsSocketOnlyFileProgress(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	err = writer.Append(Event{
		Type: EventType("file_progress"),
		At:   testEventTime(1),
		Seq:  1,
	})
	assertExactError(
		t,
		err,
		"backupplan: file_progress is socket-only and must not be logged",
	)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := len(readSessionLines(t, root, testSessionID)), 1; got != want {
		t.Fatalf("line count = %d, want header only (%d)", got, want)
	}
}

func TestSessionReadMissingTerminal(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	event := Event{Type: EventPlanStarted, At: testEventTime(1), Seq: 1, PlanID: testPlanID}
	if err := writer.Append(event); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal != nil {
		t.Fatalf("Terminal = %#v, want nil", session.Terminal)
	}
	if !reflect.DeepEqual(session.Events, []Event{event}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{event})
	}
}

func TestSessionReadIgnoresTruncatedFinalLine(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	event := Event{Type: EventPlanStarted, At: testEventTime(1), Seq: 1, PlanID: testPlanID}
	if err := writer.Append(event); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	file, err := os.OpenFile(
		sessionFile(root, testSessionID),
		os.O_APPEND|os.O_WRONLY,
		0o664,
	)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString(`{"type":"plan_completed","at":"2026-07-27T09:20:03Z","seq":2`); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close truncated file: %v", err)
	}

	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal != nil {
		t.Fatalf("Terminal = %#v, want nil", session.Terminal)
	}
	if !reflect.DeepEqual(session.Events, []Event{event}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{event})
	}
}

func TestSessionReadReturnsParsedPrefixWithError(t *testing.T) {
	root := t.TempDir()
	header := validHeader()
	writer, err := CreateSession(root, header)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	event := Event{
		Type:   EventPlanStarted,
		At:     testEventTime(1),
		Seq:    1,
		PlanID: testPlanID,
	}
	if err := writer.Append(event); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	file, err := os.OpenFile(
		sessionFile(root, header.SessionID),
		os.O_APPEND|os.O_WRONLY,
		0o664,
	)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if err := json.NewEncoder(file).Encode(headerToWire(header)); err != nil {
		t.Fatalf("Encode duplicate header: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close duplicate header file: %v", err)
	}

	session, err := ReadSession(root, header.SessionID)
	assertExactError(t, err, "backupplan: duplicate session header")
	if !reflect.DeepEqual(session.Header, header) {
		t.Fatalf("Header = %#v, want %#v", session.Header, header)
	}
	if !reflect.DeepEqual(session.Events, []Event{event}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{event})
	}
}

func TestSessionSchemaVersionGate(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	path := sessionFile(root, testSessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	data = bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1)
	if err := os.WriteFile(path, data, 0o664); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = ReadSession(root, testSessionID)
	assertExactError(t, err, "backupplan: unsupported session schemaVersion 2")
}

func TestSessionRejectsSchemaVersionOnEvent(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value string
	}{
		{name: "canonical", field: "schemaVersion", value: "1"},
		{name: "mixed case", field: "SchemaVersion", value: "1"},
		{name: "null", field: "schemaVersion", value: "null"},
		{name: "mixed case null", field: "SCHEMAVERSION", value: "null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writer, err := CreateSession(root, validHeader())
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			file, err := os.OpenFile(
				sessionFile(root, testSessionID),
				os.O_APPEND|os.O_WRONLY,
				0o664,
			)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			if _, err := fmt.Fprintf(
				file,
				`{"type":"plan_started",%q:%s,"at":"2026-07-27T09:20:02Z","seq":1}`+"\n",
				tc.field,
				tc.value,
			); err != nil {
				t.Fatalf("WriteString: %v", err)
			}
			if err := file.Close(); err != nil {
				t.Fatalf("Close event file: %v", err)
			}

			_, err = ReadSession(root, testSessionID)
			assertExactError(
				t,
				err,
				"backupplan: schemaVersion is only allowed in the session header",
			)
		})
	}
}

func TestSessionHeaderValidationOnEncodeAndDecode(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*Header)
		mutateWire func(*sessionHeaderWire)
		want       string
	}{
		{
			name: "sessionID",
			mutate: func(header *Header) {
				header.SessionID = "bad"
			},
			mutateWire: func(wire *sessionHeaderWire) {
				wire.SessionID = "bad"
			},
			want: `backupplan: sessionID "bad" must be a 26-character uppercase ` +
				"Crockford base32 ULID",
		},
		{
			name: "executor version",
			mutate: func(header *Header) {
				header.Executor.Version = ""
			},
			mutateWire: func(wire *sessionHeaderWire) {
				wire.Executor.Version = ""
			},
			want: "backupplan: executor.version is required",
		},
		{
			name: "executor host",
			mutate: func(header *Header) {
				header.Executor.Host = ""
			},
			mutateWire: func(wire *sessionHeaderWire) {
				wire.Executor.Host = ""
			},
			want: "backupplan: executor.host is required",
		},
		{
			name: "startedAt",
			mutate: func(header *Header) {
				header.StartedAt = time.Time{}
			},
			mutateWire: func(wire *sessionHeaderWire) {
				wire.StartedAt = ""
			},
			want: "backupplan: startedAt is required",
		},
		{
			name: "ready planID",
			mutate: func(header *Header) {
				header.ReadyPlans[0] = "bad"
			},
			mutateWire: func(wire *sessionHeaderWire) {
				wire.ReadyPlans[0] = "bad"
			},
			want: `backupplan: ready planID "bad" must be a 26-character uppercase ` +
				"Crockford base32 ULID",
		},
		{
			name: "duplicate ready planID",
			mutate: func(header *Header) {
				header.ReadyPlans[1] = header.ReadyPlans[0]
			},
			mutateWire: func(wire *sessionHeaderWire) {
				wire.ReadyPlans[1] = wire.ReadyPlans[0]
			},
			want: `backupplan: duplicate ready planID "` + testPlanID + `"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := validHeader()
			tc.mutate(&header)
			_, err := CreateSession(t.TempDir(), header)
			assertExactError(t, err, tc.want)

			wire := headerToWire(validHeader())
			tc.mutateWire(&wire)
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var decoded sessionHeaderWire
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			_, err = headerFromWire(decoded)
			assertExactError(t, err, tc.want)
		})
	}
}

func TestSessionEventValidationOnEncodeAndDecode(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Event)
		mutateRaw func(*eventWire)
		want      string
	}{
		{
			name: "event type",
			mutate: func(event *Event) {
				event.Type = "unknown"
			},
			mutateRaw: func(wire *eventWire) {
				wire.Type = "unknown"
			},
			want: `backupplan: unsupported session event type "unknown"`,
		},
		{
			name: "at required",
			mutate: func(event *Event) {
				event.At = time.Time{}
			},
			mutateRaw: func(wire *eventWire) {
				wire.At = ""
			},
			want: "backupplan: event at is required",
		},
		{
			name: "at UTC",
			mutate: func(event *Event) {
				event.At = time.Date(
					2026, 7, 27, 2, 20, 2, 0,
					time.FixedZone("PDT", -7*60*60),
				)
			},
			mutateRaw: func(wire *eventWire) {
				wire.At = "2026-07-27T02:20:02-07:00"
			},
			want: "backupplan: event at must be UTC",
		},
		{
			name: "at whole seconds",
			mutate: func(event *Event) {
				event.At = event.At.Add(time.Nanosecond)
			},
			mutateRaw: func(wire *eventWire) {
				wire.At = "2026-07-27T09:20:02.000000001Z"
			},
			want: "backupplan: event at must be truncated to whole seconds",
		},
		{
			name: "seq",
			mutate: func(event *Event) {
				event.Seq = 0
			},
			mutateRaw: func(wire *eventWire) {
				wire.Seq = 0
			},
			want: "backupplan: event seq must be greater than zero",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writer, err := CreateSession(root, validHeader())
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			event := Event{
				Type:   EventPlanStarted,
				At:     testEventTime(1),
				Seq:    1,
				PlanID: testPlanID,
			}
			tc.mutate(&event)
			err = writer.Append(event)
			assertExactError(t, err, tc.want)
			if err := writer.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			wire := toEventWire(Event{
				Type:   EventPlanStarted,
				At:     testEventTime(1),
				Seq:    1,
				PlanID: testPlanID,
			})
			tc.mutateRaw(&wire)
			_, err = eventFromWire(wire)
			assertExactError(t, err, tc.want)
		})
	}
}

func TestSessionEventPayloadValidationOnEncodeAndDecode(t *testing.T) {
	cases := []struct {
		name      string
		eventType EventType
		mutate    func(*Event)
		want      string
	}{
		{
			name:      "plan_started requires planId",
			eventType: EventPlanStarted,
			mutate:    func(event *Event) { event.PlanID = "" },
			want:      "backupplan: planID is required",
		},
		{
			name:      "planId canonical",
			eventType: EventPlanStarted,
			mutate:    func(event *Event) { event.PlanID = "../../etc/passwd" },
			want: `backupplan: planID "../../etc/passwd" must be a 26-character ` +
				"uppercase Crockford base32 ULID",
		},
		{
			name:      "tapeId",
			eventType: EventTapeLoaded,
			mutate:    func(event *Event) { event.TapeID = "nope" },
			want:      `backupplan: tapeID "nope" must be exactly 8 characters`,
		},
		{
			name:      "volumeId",
			eventType: EventTapeLoaded,
			mutate:    func(event *Event) { event.VolumeID = "nope" },
			want: `backupplan: volumeID "nope" must be a 26-character uppercase ` +
				"Crockford base32 ULID",
		},
		{
			name:      "snapshot",
			eventType: EventItemCompleted,
			mutate:    func(event *Event) { event.Snapshot = "not a snapshot" },
			want: `backupplan: snapshot "not a snapshot": snapshot name ` +
				`"not a snapshot" is missing .g<generation>`,
		},
		{
			name:      "bytes",
			eventType: EventItemStarted,
			mutate:    func(event *Event) { event.Bytes = -5 },
			want:      "backupplan: bytes must not be negative",
		},
		{
			name:      "itemsWritten",
			eventType: EventPlanCompleted,
			mutate:    func(event *Event) { event.ItemsWritten = -1 },
			want:      "backupplan: itemsWritten must not be negative",
		},
		{
			name:      "files",
			eventType: EventItemStarted,
			mutate:    func(event *Event) { event.Files = -3 },
			want:      "backupplan: files must not be negative",
		},
		{
			name:      "path valid UTF-8",
			eventType: EventFileStarted,
			mutate:    func(event *Event) { event.Path = "Season 1/a\xffb.mkv" },
			want:      "backupplan: path must be valid UTF-8",
		},
		{
			name:      "waiting_for_tape forbids planId",
			eventType: EventWaitingForTape,
			mutate:    func(event *Event) { event.PlanID = testPlanID },
			want:      "backupplan: waiting_for_tape event must not contain planId",
		},
		{
			name:      "waiting_for_tape forbids path",
			eventType: EventWaitingForTape,
			mutate:    func(event *Event) { event.Path = "Season 1/file.mkv" },
			want:      "backupplan: waiting_for_tape event must not contain path",
		},
		{
			name:      "waiting_for_tape forbids snapshot",
			eventType: EventWaitingForTape,
			mutate:    func(event *Event) { event.Snapshot = testSnapshot },
			want:      "backupplan: waiting_for_tape event must not contain snapshot",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSessionEventEncodeAndDecodeRejection(
				t,
				validEvent(tc.eventType),
				tc.mutate,
				tc.want,
			)
		})
	}
}

func TestSessionItemReasonConstantsAcceptedByAllCarriers(t *testing.T) {
	for _, reason := range validItemReasons() {
		for _, eventType := range []EventType{EventItemFailed, EventItemSkipped} {
			t.Run(string(eventType)+"/"+string(reason), func(t *testing.T) {
				event := validEvent(eventType)
				event.Reason = string(reason)
				if err := validateEvent(event); err != nil {
					t.Fatalf("validateEvent: %v", err)
				}
				if _, err := eventFromWire(toEventWire(event)); err != nil {
					t.Fatalf("eventFromWire: %v", err)
				}
			})
		}
		t.Run("freshness_checked/"+string(reason), func(t *testing.T) {
			event := validEvent(EventFreshnessChecked)
			event.Dropped[0].Reason = string(reason)
			if err := validateEvent(event); err != nil {
				t.Fatalf("validateEvent: %v", err)
			}
			if _, err := eventFromWire(toEventWire(event)); err != nil {
				t.Fatalf("eventFromWire: %v", err)
			}
		})
	}
}

func TestSessionItemReasonCarriersRejectUnknownReason(t *testing.T) {
	cases := []struct {
		name   string
		event  Event
		mutate func(*Event)
	}{
		{
			name:   "item_failed",
			event:  validEvent(EventItemFailed),
			mutate: func(event *Event) { event.Reason = "unknown" },
		},
		{
			name:   "item_skipped",
			event:  validEvent(EventItemSkipped),
			mutate: func(event *Event) { event.Reason = "unknown" },
		},
		{
			name:  "freshness_checked",
			event: validEvent(EventFreshnessChecked),
			mutate: func(event *Event) {
				event.Dropped[0].Reason = "unknown"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSessionEventEncodeAndDecodeRejection(
				t,
				tc.event,
				tc.mutate,
				`backupplan: unsupported item reason "unknown"`,
			)
		})
	}
}

func TestSessionDivergenceExtraSnapshotsValidation(t *testing.T) {
	cases := []struct {
		name   string
		extras []string
		want   string
	}{
		{
			name:   "canonical",
			extras: []string{strings.ToLower(testSnapshot)},
			want: `backupplan: snapshot "or3giyr2gm3tambxga.g7": snapshot name ` +
				`"or3giyr2gm3tambxga.g7" has invalid base32 metadataRef: ` +
				"illegal base32 data at input byte 0",
		},
		{
			name:   "duplicate",
			extras: []string{testSnapshot, testSnapshot},
			want:   `backupplan: duplicate snapshot "OR3GIYR2GM3TAMBXGA.g7"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSessionEventEncodeAndDecodeRejection(
				t,
				validEvent(EventDivergenceChecked),
				func(event *Event) { event.ExtraSnapshots = tc.extras },
				tc.want,
			)
		})
	}
}

func TestSessionEventRequiredFieldsByType(t *testing.T) {
	cases := []struct {
		eventType EventType
		mutate    func(*Event)
		want      string
	}{
		{EventWaitingForTape, func(event *Event) { event.Candidates = nil },
			"backupplan: candidates must contain at least one tapeID"},
		{EventTapeLoaded, func(event *Event) { event.TapeID = "" },
			"backupplan: tapeID is required"},
		{EventTapeRejected, func(event *Event) { event.Reason = "" },
			"backupplan: reason is required"},
		{EventPlanStarted, func(event *Event) { event.PlanID = "" },
			"backupplan: planID is required"},
		{EventPlanCompleted, func(event *Event) { event.PlanID = "" },
			"backupplan: planID is required"},
		{EventPlanFailed, func(event *Event) { event.Reason = "" },
			"backupplan: reason is required"},
		{EventPlanCancelled, func(event *Event) { event.Reason = "" },
			"backupplan: reason is required"},
		{EventEncryptionVerified, func(event *Event) { event.PlanID = "" },
			"backupplan: planID is required"},
		{EventSnapshotSwept, func(event *Event) { event.Entry = "" },
			"backupplan: entry is required"},
		{EventDivergenceChecked, func(event *Event) { event.Result = "" },
			"backupplan: result is required"},
		{EventFreshnessChecked, func(event *Event) { event.PlanID = "" },
			"backupplan: planID is required"},
		{EventItemStarted, func(event *Event) { event.MetadataRef = "" },
			`backupplan: event item ("", 8): metadataRef is required`},
		{EventItemCompleted, func(event *Event) { event.Snapshot = "" },
			`backupplan: snapshot "": snapshot name "" is missing .g<generation>`},
		{EventItemFailed, func(event *Event) { event.Reason = "" },
			"backupplan: reason is required"},
		{EventItemSkipped, func(event *Event) { event.Reason = "" },
			"backupplan: reason is required"},
		{EventFileStarted, func(event *Event) { event.Path = "" },
			"backupplan: path is required"},
		{EventFileCompleted, func(event *Event) { event.Path = "" },
			"backupplan: path is required"},
		{EventCancelRequested, func(event *Event) { event.PlanID = "" },
			"backupplan: planID is required"},
		{EventTerminal, func(event *Event) { event.State = "" },
			"backupplan: state is required"},
	}

	for _, tc := range cases {
		t.Run(string(tc.eventType), func(t *testing.T) {
			assertSessionEventEncodeAndDecodeRejection(
				t,
				validEvent(tc.eventType),
				tc.mutate,
				tc.want,
			)
		})
	}
}

func TestSessionEventForbiddenFieldsByType(t *testing.T) {
	cases := []struct {
		eventType EventType
		field     string
		mutate    func(*Event)
	}{
		{EventWaitingForTape, "planId", func(event *Event) { event.PlanID = testPlanID }},
		{EventTapeLoaded, "planId", func(event *Event) { event.PlanID = testPlanID }},
		{EventTapeRejected, "planId", func(event *Event) { event.PlanID = testPlanID }},
		{EventPlanStarted, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventPlanCompleted, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventPlanFailed, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventPlanCancelled, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventEncryptionVerified, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventSnapshotSwept, "planId", func(event *Event) { event.PlanID = testPlanID }},
		{EventDivergenceChecked, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventFreshnessChecked, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventItemStarted, "snapshot", func(event *Event) { event.Snapshot = testSnapshot }},
		{EventItemCompleted, "result", func(event *Event) { event.Result = "match" }},
		{EventItemFailed, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventItemSkipped, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventFileStarted, "snapshot", func(event *Event) { event.Snapshot = testSnapshot }},
		{EventFileCompleted, "snapshot", func(event *Event) { event.Snapshot = testSnapshot }},
		{EventCancelRequested, "path", func(event *Event) { event.Path = "file.mkv" }},
		{EventTerminal, "planId", func(event *Event) { event.PlanID = testPlanID }},
	}

	for _, tc := range cases {
		t.Run(string(tc.eventType), func(t *testing.T) {
			want := fmt.Sprintf(
				"backupplan: %s event must not contain %s",
				tc.eventType,
				tc.field,
			)
			assertSessionEventEncodeAndDecodeRejection(
				t,
				validEvent(tc.eventType),
				tc.mutate,
				want,
			)
		})
	}
}

func TestSessionDecodeRejectsEveryForbiddenFieldAtZeroValue(t *testing.T) {
	testSessionDecodeRejectsEveryForbiddenField(
		t,
		"canonical",
		func(name string) string { return name },
		func(value json.RawMessage) json.RawMessage { return value },
	)
}

func TestSessionDecodeRejectsEveryForbiddenFieldInMixedCase(t *testing.T) {
	testSessionDecodeRejectsEveryForbiddenField(
		t,
		"mixed_case",
		func(name string) string {
			return strings.ToUpper(name[:1]) + name[1:]
		},
		func(value json.RawMessage) json.RawMessage { return value },
	)
}

func TestSessionDecodeRejectsEveryForbiddenFieldAsNull(t *testing.T) {
	testSessionDecodeRejectsEveryForbiddenField(
		t,
		"null",
		func(name string) string { return name },
		func(json.RawMessage) json.RawMessage { return json.RawMessage("null") },
	)
}

func TestSessionDecodeRejectsDuplicateCaseFoldedEventFields(t *testing.T) {
	for _, event := range allDurableEvents() {
		data, err := json.Marshal(toEventWire(event))
		if err != nil {
			t.Fatalf("Marshal %s event: %v", event.Type, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("Unmarshal %s event: %v", event.Type, err)
		}
		for name, value := range fields {
			for _, nullFirst := range []bool{false, true} {
				order := "null_last"
				if nullFirst {
					order = "null_first"
				}
				t.Run(string(event.Type)+"/"+name+"/"+order, func(t *testing.T) {
					eventFields := make(map[string]json.RawMessage, len(fields)-1)
					for fieldName, fieldValue := range fields {
						if fieldName != name {
							eventFields[fieldName] = fieldValue
						}
					}
					eventData := jsonObjectWithDuplicateFoldedField(
						t,
						eventFields,
						name,
						value,
						nullFirst,
					)

					_, err := readSessionEventLines(t, eventData)
					first := strings.ToUpper(name[:1]) + name[1:]
					want := fmt.Sprintf(
						"backupplan: session line 2: duplicate case-folded JSON fields at %s: %q and %q",
						name,
						first,
						name,
					)
					assertExactError(t, err, want)
				})
			}
		}
	}
}

func testSessionDecodeRejectsEveryForbiddenField(
	t *testing.T,
	variant string,
	fieldName func(string) string,
	fieldValue func(json.RawMessage) json.RawMessage,
) {
	t.Helper()
	tested := 0
	for _, event := range allDurableEvents() {
		allowed := make(map[string]struct{}, len(sessionEventFieldsByType[event.Type]))
		for _, field := range sessionEventFieldsByType[event.Type] {
			allowed[field] = struct{}{}
		}
		for _, field := range zeroValueEventFields {
			if _, exists := allowed[field.name]; exists {
				continue
			}
			tested++
			t.Run(string(event.Type)+"/"+field.name+"/"+variant, func(t *testing.T) {
				data, err := json.Marshal(toEventWire(event))
				if err != nil {
					t.Fatalf("Marshal event: %v", err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(data, &fields); err != nil {
					t.Fatalf("Unmarshal event fields: %v", err)
				}
				fields[fieldName(field.name)] = fieldValue(field.value)
				data, err = json.Marshal(fields)
				if err != nil {
					t.Fatalf("Marshal mutated event: %v", err)
				}

				_, err = readSessionEventLines(t, data)
				want := fmt.Sprintf(
					"backupplan: %s event must not contain %s",
					event.Type,
					field.name,
				)
				assertExactError(t, err, want)
			})
		}
	}
	const wantCases = 346
	if tested != wantCases {
		t.Fatalf("forbidden zero-value cases = %d, want %d", tested, wantCases)
	}
}

func TestSessionReadRejectsNonIncreasingSequence(t *testing.T) {
	cases := []struct {
		name     string
		previous uint64
		current  uint64
		want     string
	}{
		{
			name:     "descending",
			previous: 2,
			current:  1,
			want:     "backupplan: session seq 1 is not greater than previous seq 2",
		},
		{
			name:     "duplicate",
			previous: 2,
			current:  2,
			want:     "backupplan: session seq 2 is not greater than previous seq 2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := validEvent(EventPlanStarted)
			first.Seq = tc.previous
			second := validEvent(EventEncryptionVerified)
			second.Seq = tc.current
			lines := [][]byte{
				marshalEventLine(t, first),
				marshalEventLine(t, second),
			}
			root := t.TempDir()
			writeSessionEventLines(t, root, lines...)
			session, err := ReadSession(root, testSessionID)
			assertExactError(t, err, tc.want)
			writer, err := OpenSession(root, testSessionID)
			if err == nil {
				_ = writer.Close()
			}
			assertExactError(t, err, tc.want)
			if !reflect.DeepEqual(session.Events, []Event{first}) {
				t.Fatalf("parsed prefix = %#v, want %#v", session.Events, []Event{first})
			}
		})
	}
}

func TestSessionReadRejectsCompleteRecordAfterTerminal(t *testing.T) {
	terminal := validEvent(EventTerminal)
	following := validEvent(EventPlanStarted)
	following.Seq = 2
	following.At = testEventTime(2)
	lines := [][]byte{
		marshalEventLine(t, terminal),
		marshalEventLine(t, following),
	}
	root := t.TempDir()
	writeSessionEventLines(t, root, lines...)
	session, err := ReadSession(root, testSessionID)
	want := "backupplan: session line 3 follows terminal seq 1"
	assertExactError(t, err, want)
	writer, err := OpenSession(root, testSessionID)
	if err == nil {
		_ = writer.Close()
	}
	assertExactError(t, err, want)
	if session.Terminal == nil || !reflect.DeepEqual(*session.Terminal, terminal) {
		t.Fatalf("Terminal = %#v, want %#v", session.Terminal, terminal)
	}
}

func TestSessionReadIgnoresTruncatedFragmentAfterTerminal(t *testing.T) {
	terminal := validEvent(EventTerminal)
	data := sessionEventLogBytes(t, marshalEventLine(t, terminal))
	data = append(data, `{"type":"plan_started"`...)
	session, err := readSessionRecords(
		bufio.NewReader(bytes.NewReader(data)),
		testSessionID,
	)
	if err != nil {
		t.Fatalf("readSessionRecords: %v", err)
	}
	if session.Terminal == nil || !reflect.DeepEqual(*session.Terminal, terminal) {
		t.Fatalf("Terminal = %#v, want %#v", session.Terminal, terminal)
	}
}

func TestSessionReadAllowsEmptyLineBeforeEvent(t *testing.T) {
	event := validEvent(EventPlanStarted)
	session, err := readSessionEventLines(
		t,
		nil,
		marshalEventLine(t, event),
	)
	if err != nil {
		t.Fatalf("readSessionRecords: %v", err)
	}
	if !reflect.DeepEqual(session.Events, []Event{event}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{event})
	}
}

func TestSessionReadAllowsBlankLinesBeforeAndAfterTerminal(t *testing.T) {
	event := validEvent(EventPlanStarted)
	terminal := validEvent(EventTerminal)
	terminal.Seq = 2
	terminal.At = testEventTime(2)
	session, err := readSessionEventLines(
		t,
		nil,
		[]byte(" \t "),
		marshalEventLine(t, event),
		marshalEventLine(t, terminal),
		nil,
		[]byte(" \t "),
	)
	if err != nil {
		t.Fatalf("readSessionRecords: %v", err)
	}
	if !reflect.DeepEqual(session.Events, []Event{event}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{event})
	}
	if session.Terminal == nil || !reflect.DeepEqual(*session.Terminal, terminal) {
		t.Fatalf("Terminal = %#v, want %#v", session.Terminal, terminal)
	}
}

func TestSessionIDMustMatchFilename(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(sessionFile(root, testSessionID))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(sessionFile(root, secondPlanID), data, 0o664); err != nil {
		t.Fatalf("WriteFile copy: %v", err)
	}

	_, err = ReadSession(root, secondPlanID)
	want := `backupplan: sessionID mismatch: filename is "` + secondPlanID +
		`", header contains "` + testSessionID + `"`
	assertExactError(t, err, want)
	_, err = OpenSession(root, secondPlanID)
	assertExactError(t, err, want)
}

func TestSessionCreateRefusesExistingFile(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	_, err = CreateSession(root, validHeader())
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateSession second error = %v, want os.ErrExist", err)
	}
}

func TestSessionOpenExcludesConcurrentWriterUntilClose(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close created writer: %v", err)
	}

	first, err := OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession first: %v", err)
	}
	_, err = OpenSession(root, testSessionID)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("OpenSession second error = %v, want os.ErrExist", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close first resumed writer: %v", err)
	}

	second, err := OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession after Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close second resumed writer: %v", err)
	}
}

func TestSessionOpenSucceedsAfterHardKilledLockHolder(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close created writer: %v", err)
	}

	command := exec.Command(
		os.Args[0],
		"-test.run=^TestSessionLockHolderProcess$",
		"--",
		root,
	)
	command.Env = append(os.Environ(), "KURA_TEST_SESSION_LOCK_HOLDER=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("Start lock holder: %v", err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Signal(syscall.SIGKILL)
			_ = command.Wait()
		}
	}()

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		waitErr := command.Wait()
		t.Fatalf("lock holder did not report readiness: scan error = %v, wait error = %v",
			scanner.Err(), waitErr)
	}
	if got := scanner.Text(); got != "locked" {
		t.Fatalf("lock holder readiness = %q, want %q", got, "locked")
	}

	_, err = OpenSession(root, testSessionID)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("OpenSession while child holds lock error = %v, want os.ErrExist", err)
	}

	if err := command.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL lock holder: %v", err)
	}
	waitErr := command.Wait()
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait after SIGKILL error = %v, want *exec.ExitError", waitErr)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("lock holder wait status = %#v, want SIGKILL", exitErr.Sys())
	}

	if _, err := ReadSession(root, testSessionID); err != nil {
		t.Fatalf("ReadSession after SIGKILL: %v", err)
	}
	writer, err = OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession after SIGKILL: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}
}

func TestSessionLockHolderProcess(t *testing.T) {
	if os.Getenv("KURA_TEST_SESSION_LOCK_HOLDER") != "1" {
		return
	}
	root := os.Args[len(os.Args)-1]
	writer, err := OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession lock holder: %v", err)
	}
	fmt.Println("locked")
	<-make(chan struct{})
	runtime.KeepAlive(writer)
}

func TestSessionOpenTerminalLogDoesNotRetainLock(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Append(validEvent(EventTerminal)); err != nil {
		t.Fatalf("Append terminal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close created writer: %v", err)
	}

	first, err := OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession first: %v", err)
	}
	second, err := OpenSession(root, testSessionID)
	if err != nil {
		_ = first.Close()
		t.Fatalf("OpenSession second: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
}

func TestSessionWriterCloseIsIdempotent(t *testing.T) {
	writer, err := CreateSession(t.TempDir(), validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Append(Event{
		Type:   EventPlanStarted,
		At:     testEventTime(2),
		Seq:    2,
		PlanID: testPlanID,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}
	err = writer.Append(Event{
		Type:   EventPlanStarted,
		At:     testEventTime(1),
		Seq:    1,
		PlanID: testPlanID,
	})
	assertExactError(t, err, "backupplan: append to closed session")
}

func TestSessionSyncFailureReplayMustRetrySyncWithoutDuplicateLine(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	syncCalls := 0
	writer.sync = func() error {
		syncCalls++
		return errors.New("injected sync failure")
	}
	event := validEvent(EventPlanStarted)
	err = writer.Append(event)
	assertExactError(
		t,
		err,
		"backupplan: sync session event: injected sync failure",
	)
	if syncCalls != 1 {
		t.Fatalf("sync calls after first append = %d, want 1", syncCalls)
	}
	if writer.highestSeq != event.Seq {
		t.Fatalf("highestSeq = %d, want %d", writer.highestSeq, event.Seq)
	}
	err = writer.Append(event)
	assertExactError(
		t,
		err,
		"backupplan: sync session event: injected sync failure",
	)
	if syncCalls != 2 {
		t.Fatalf("sync calls after replay = %d, want 2", syncCalls)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readSessionLines(t, root, testSessionID)
	if got, want := len(lines), 2; got != want {
		t.Fatalf("line count after replay = %d, want %d", got, want)
	}
	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !reflect.DeepEqual(session.Events, []Event{event}) {
		t.Fatalf("Events = %#v, want %#v", session.Events, []Event{event})
	}
	resumed, err := OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}
}

func TestSessionTerminalSyncFailureRejectsFollowingEventAndLeavesLogLoadable(t *testing.T) {
	root := t.TempDir()
	writer, err := CreateSession(root, validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	writer.sync = func() error {
		return errors.New("injected sync failure")
	}
	terminal := validEvent(EventTerminal)
	err = writer.Append(terminal)
	assertExactError(
		t,
		err,
		"backupplan: sync session event: injected sync failure",
	)
	if writer.highestSeq != terminal.Seq {
		t.Fatalf("highestSeq = %d, want %d", writer.highestSeq, terminal.Seq)
	}
	if !writer.terminal {
		t.Fatal("terminal = false, want true")
	}

	following := validEvent(EventPlanStarted)
	following.Seq = terminal.Seq + 1
	following.At = testEventTime(2)
	err = writer.Append(following)
	if !errors.Is(err, ErrSessionTerminal) {
		t.Fatalf("Append following event error = %v, want ErrSessionTerminal", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	session, err := ReadSession(root, testSessionID)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.Terminal == nil || !reflect.DeepEqual(*session.Terminal, terminal) {
		t.Fatalf("Terminal = %#v, want %#v", session.Terminal, terminal)
	}
	resumed, err := OpenSession(root, testSessionID)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatalf("Close resumed writer: %v", err)
	}
}

func TestSessionRejectsAppendAfterTerminal(t *testing.T) {
	writer, err := CreateSession(t.TempDir(), validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := writer.Append(Event{
		Type:           EventTerminal,
		At:             testEventTime(1),
		Seq:            1,
		State:          "ended",
		Reason:         "idle_timeout",
		PlansCompleted: 1,
	}); err != nil {
		t.Fatalf("Append terminal: %v", err)
	}
	err = writer.Append(Event{
		Type:   EventPlanStarted,
		At:     testEventTime(2),
		Seq:    2,
		PlanID: testPlanID,
	})
	assertExactError(t, err, "backupplan: append after terminal event")
	if !errors.Is(err, ErrSessionTerminal) {
		t.Fatalf("Append error = %v, want ErrSessionTerminal", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func validHeader() Header {
	return Header{
		SessionID: testSessionID,
		Executor: Creator{
			Version: "v0.6.0",
			Host:    "tape-vm",
		},
		StartedAt:  time.Date(2026, 7, 27, 9, 20, 1, 0, time.UTC),
		ReadyPlans: []string{testPlanID, secondPlanID},
	}
}

func allDurableEvents() []Event {
	types := []EventType{
		EventWaitingForTape,
		EventTapeLoaded,
		EventTapeRejected,
		EventPlanStarted,
		EventPlanCompleted,
		EventPlanFailed,
		EventPlanCancelled,
		EventEncryptionVerified,
		EventSnapshotSwept,
		EventOutboxOverflow,
		EventDivergenceChecked,
		EventFreshnessChecked,
		EventItemStarted,
		EventItemCompleted,
		EventItemFailed,
		EventItemSkipped,
		EventFileStarted,
		EventFileCompleted,
		EventCancelRequested,
		EventTerminal,
	}
	events := make([]Event, 0, len(types))
	for i, eventType := range types {
		event := Event{
			Type: eventType,
			At:   testEventTime(i + 1),
			Seq:  uint64(i + 1),
		}
		switch eventType {
		case EventWaitingForTape:
			event.Candidates = []tape.ID{"ABC123L6", "DEF456L6"}
		case EventTapeLoaded:
			event.TapeID = tape.ID("ABC123L6")
			event.VolumeID = volume.ID(testVolumeID)
		case EventTapeRejected:
			event.TapeID = tape.ID("DEF456L6")
			event.Reason = "wrong_volume"
			event.Detail = "expected another cartridge"
		case EventPlanStarted, EventEncryptionVerified, EventCancelRequested:
			event.PlanID = testPlanID
		case EventPlanCompleted:
			event.PlanID = testPlanID
			event.ItemsWritten = 1
			event.ItemsFailed = 1
		case EventPlanFailed, EventPlanCancelled:
			event.PlanID = testPlanID
			event.Reason = "cancelled"
			event.Detail = "operator requested cancellation"
		case EventSnapshotSwept:
			event.Entry = "broken-entry"
			event.Reason = "unparseable_name"
		case EventDivergenceChecked:
			event.PlanID = testPlanID
			event.Result = "match"
			event.ExtraSnapshots = []string{testSnapshot}
		case EventFreshnessChecked:
			event.PlanID = testPlanID
			event.Dropped = []DroppedItem{{
				MetadataRef: "tvdb:370070",
				Generation:  8,
				Reason:      "payload_drift",
			}}
		case EventItemStarted:
			event.PlanID = testPlanID
			event.MetadataRef = "tvdb:370070"
			event.Generation = 8
			event.Bytes = 10
			event.Files = 2
		case EventItemCompleted:
			event.PlanID = testPlanID
			event.MetadataRef = "tvdb:370070"
			event.Generation = 8
			event.Snapshot = "OR3GIYR2GM3TAMBXGA.g8"
		case EventItemFailed, EventItemSkipped:
			event.PlanID = testPlanID
			event.MetadataRef = "tvdb:370070"
			event.Generation = 8
			event.Reason = "payload_drift"
			event.Detail = "payload changed after approval"
		case EventFileStarted:
			event.PlanID = testPlanID
			event.MetadataRef = "tvdb:370070"
			event.Bytes = 10
			event.Path = "Season 1/Show - S01E01.mkv"
		case EventFileCompleted:
			event.PlanID = testPlanID
			event.MetadataRef = "tvdb:370070"
			event.Path = "Season 1/Show - S01E01.mkv"
		case EventTerminal:
			event.State = "ended"
			event.Reason = "idle_timeout"
			event.PlansCompleted = 1
		}
		events = append(events, event)
	}
	return events
}

func validEvent(eventType EventType) Event {
	for _, event := range allDurableEvents() {
		if event.Type == eventType {
			event.Seq = 1
			event.At = testEventTime(1)
			return event
		}
	}
	panic("missing valid event fixture for " + string(eventType))
}

func validItemReasons() []ItemReason {
	return []ItemReason{
		ReasonPayloadDrift,
		ReasonClaimStale,
		ReasonInsufficientSpace,
		ReasonChecksumMismatch,
		ReasonAlreadyPresent,
		ReasonUnsupportedFileType,
		ReasonSeriesMoved,
		ReasonSeriesRootMissing,
		ReasonStagedIntent,
		ReasonWriteFailed,
	}
}

func assertSessionEventEncodeAndDecodeRejection(
	t *testing.T,
	event Event,
	mutate func(*Event),
	want string,
) {
	t.Helper()
	mutate(&event)
	writer, err := CreateSession(t.TempDir(), validHeader())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	err = writer.Append(event)
	assertExactError(t, err, want)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = eventFromWire(toEventWire(event))
	assertExactError(t, err, want)
}

func testEventTime(second int) time.Time {
	return time.Date(2026, 7, 27, 9, 20, second, 0, time.UTC)
}

func removeSessionTrailingNewline(t *testing.T, root string) {
	t.Helper()
	path := sessionFile(root, testSessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile session: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("session does not end in a newline: %q", data)
	}
	if err := os.Truncate(path, int64(len(data)-1)); err != nil {
		t.Fatalf("Truncate trailing newline: %v", err)
	}
}

func eventSeqs(session Session) []uint64 {
	seqs := make([]uint64, 0, len(session.Events)+1)
	for _, event := range session.Events {
		seqs = append(seqs, event.Seq)
	}
	if session.Terminal != nil {
		seqs = append(seqs, session.Terminal.Seq)
	}
	return seqs
}

func marshalEventLine(t *testing.T, event Event) []byte {
	t.Helper()
	data, err := json.Marshal(toEventWire(event))
	if err != nil {
		t.Fatalf("Marshal event: %v", err)
	}
	return data
}

func readSessionEventLines(t *testing.T, lines ...[]byte) (Session, error) {
	t.Helper()
	data := sessionEventLogBytes(t, lines...)
	return readSessionRecords(bufio.NewReader(bytes.NewReader(data)), testSessionID)
}

func writeSessionEventLines(t *testing.T, root string, lines ...[]byte) {
	t.Helper()
	if err := os.MkdirAll(sessionsDir(root), 0o775); err != nil {
		t.Fatalf("MkdirAll sessions: %v", err)
	}
	if err := os.WriteFile(
		sessionFile(root, testSessionID),
		sessionEventLogBytes(t, lines...),
		0o664,
	); err != nil {
		t.Fatalf("WriteFile session: %v", err)
	}
}

func sessionEventLogBytes(t *testing.T, lines ...[]byte) []byte {
	t.Helper()
	header, err := json.Marshal(headerToWire(validHeader()))
	if err != nil {
		t.Fatalf("Marshal header: %v", err)
	}
	var data bytes.Buffer
	data.Write(header)
	data.WriteByte('\n')
	for _, line := range lines {
		data.Write(line)
		data.WriteByte('\n')
	}
	return data.Bytes()
}

func readSessionLines(t *testing.T, root, sessionID string) [][]byte {
	t.Helper()
	file, err := os.Open(sessionFile(root, sessionID))
	if err != nil {
		t.Fatalf("Open session: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := make([][]byte, 0)
	for scanner.Scan() {
		lines = append(lines, bytes.Clone(scanner.Bytes()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan session: %v", err)
	}
	return lines
}

func headerToWire(header Header) sessionHeaderWire {
	return sessionHeaderWire{
		Type:          "header",
		SchemaVersion: schemaVersion,
		SessionID:     header.SessionID,
		Executor: creatorWire{
			Version: header.Executor.Version,
			Host:    header.Executor.Host,
		},
		StartedAt:  header.StartedAt.UTC().Format(time.RFC3339),
		ReadyPlans: copyStrings(header.ReadyPlans),
	}
}
