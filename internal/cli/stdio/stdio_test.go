package stdio

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestNewDetectsNonTerminalBuffers(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := bytes.NewBuffer(nil)
	errOut := bytes.NewBuffer(nil)

	got := New(in, out, errOut)

	if got.In != in || got.Out != out || got.Err != errOut {
		t.Fatal("New did not preserve streams")
	}
	if got.InIsTTY || got.OutIsTTY || got.IsInteractive() {
		t.Fatal("buffer streams reported as interactive")
	}
}

func TestNewDetectsNonTerminalFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stream")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer file.Close()

	got := New(file, file, file)

	if got.InIsTTY || got.OutIsTTY || got.IsInteractive() {
		t.Fatal("regular file streams reported as interactive")
	}
}

func TestWithFromRoundTrip(t *testing.T) {
	want := Stdio{InIsTTY: true, OutIsTTY: true}
	got := From(With(context.Background(), want))

	if !got.IsInteractive() {
		t.Fatal("From returned non-interactive stdio")
	}
}

func TestFromUnsetReturnsZeroValue(t *testing.T) {
	got := From(context.Background())

	if got.In != nil || got.Out != nil || got.Err != nil || got.IsInteractive() {
		t.Fatalf("From returned %#v, want zero value", got)
	}
}

func TestIsTerminalNilFile(t *testing.T) {
	if IsTerminal(nil) {
		t.Fatal("IsTerminal(nil) = true, want false")
	}
}
