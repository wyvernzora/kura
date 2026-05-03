package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/inbox"
	"github.com/wyvernzora/kura/internal/workflow"
)

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInboxList_DefaultsAndShape(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.mkv"))
	writeFile(t, filepath.Join(root, "b.mkv"))

	res, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, workflow.InboxListInput{})
	if err != nil {
		t.Fatalf("InboxList: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("entries: got %d, want 2", len(res.Entries))
	}
	for _, e := range res.Entries {
		if e.Kind != "file" {
			t.Errorf("kind: got %q", e.Kind)
		}
		if e.Path == "" {
			t.Error("path empty")
		}
		if e.MTime == "" {
			t.Errorf("mtime empty for %q", e.Path)
		}
	}
}

func TestInboxList_RequiresConfig(t *testing.T) {
	_, err := workflow.InboxList(context.Background(), workflow.Deps{}, workflow.InboxListInput{})
	if !workflow.IsInboxNotConfigured(err) {
		t.Fatalf("expected InboxNotConfiguredError, got %v", err)
	}
}

func TestInboxList_LimitCap(t *testing.T) {
	root := t.TempDir()
	in := workflow.InboxListInput{Limit: 9999}
	_, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, in)
	var tooLarge *workflow.InboxLimitTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected InboxLimitTooLargeError, got %v", err)
	}
}

func TestInboxList_DepthCap(t *testing.T) {
	root := t.TempDir()
	in := workflow.InboxListInput{Recursive: true, Depth: 99}
	_, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, in)
	var tooDeep *workflow.InboxDepthTooLargeError
	if !errors.As(err, &tooDeep) {
		t.Fatalf("expected InboxDepthTooLargeError, got %v", err)
	}
}

func TestInboxList_TruncationGeneratesHint(t *testing.T) {
	root := t.TempDir()
	for i := range 5 {
		writeFile(t, filepath.Join(root, "f"+string(rune('a'+i))+".mkv"))
	}
	in := workflow.InboxListInput{Limit: 2}
	res, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, in)
	if err != nil {
		t.Fatalf("InboxList: %v", err)
	}
	if !res.Truncated {
		t.Error("expected Truncated=true")
	}
	if res.ElidedCount != 3 {
		t.Errorf("ElidedCount: got %d, want 3", res.ElidedCount)
	}
	if len(res.Hint) == 0 {
		t.Error("expected hint when truncated")
	}
}

func TestInboxList_DirEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, workflow.InboxListInput{})
	if err != nil {
		t.Fatalf("InboxList: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries: got %d", len(res.Entries))
	}
	if res.Entries[0].Kind != "dir" {
		t.Errorf("kind: got %q, want dir", res.Entries[0].Kind)
	}
}

func TestInboxList_SurfacesPathNotFound(t *testing.T) {
	root := t.TempDir()
	in := workflow.InboxListInput{Path: "missing-dir"}
	_, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, in)
	var nfErr *inbox.PathNotFoundError
	if !errors.As(err, &nfErr) {
		t.Fatalf("expected PathNotFoundError, got %v", err)
	}
}

func TestInboxList_RejectsBadGlob(t *testing.T) {
	root := t.TempDir()
	in := workflow.InboxListInput{NameGlob: "[invalid"}
	_, err := workflow.InboxList(context.Background(), workflow.Deps{InboxRoot: root}, in)
	var glob *inbox.InvalidGlobError
	if !errors.As(err, &glob) {
		t.Fatalf("expected InvalidGlobError, got %v", err)
	}
}
