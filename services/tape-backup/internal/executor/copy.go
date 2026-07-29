package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/seriesmeta"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"golang.org/x/text/unicode/norm"
)

// Destination is the write, sync, and close contract for one copied file.
type Destination interface {
	io.Writer
	Sync() error
	Close() error
}

// DestinationOpener opens one destination file. It is the copy path's fault
// injection seam.
type DestinationOpener func(path string) (Destination, error)

// CapacityRecorder accounts for bytes consumed by a destination write.
type CapacityRecorder interface {
	RecordWrite(tape.ID, int64) error
}

// FileProgress is one lossy, socket-only copy progress frame.
type FileProgress struct {
	PlanID           string
	MetadataRef      string
	Path             string
	FileBytesWritten int64
	FileBytesTotal   int64
	ItemBytesWritten int64
	ItemBytesTotal   int64
	BytesPerSecond   float64
	At               time.Time
}

// FileProgressChannel is a bounded, lossy progress ring. Offer drops the oldest
// queued frame when the channel is full and never blocks the copy.
type FileProgressChannel struct {
	frames chan FileProgress
}

// NewFileProgressChannel creates a bounded lossy progress channel.
func NewFileProgressChannel(capacity int) (*FileProgressChannel, error) {
	if capacity < 1 {
		return nil, errors.New("executor: file progress capacity must be at least 1")
	}
	return &FileProgressChannel{
		frames: make(chan FileProgress, capacity),
	}, nil
}

// Frames returns the receive-only progress stream.
func (c *FileProgressChannel) Frames() <-chan FileProgress {
	if c == nil {
		return nil
	}
	return c.frames
}

// Offer queues the newest frame without blocking.
func (c *FileProgressChannel) Offer(progress FileProgress) {
	if c == nil {
		return
	}
	select {
	case c.frames <- progress:
		return
	default:
	}
	select {
	case <-c.frames:
	default:
	}
	select {
	case c.frames <- progress:
	default:
	}
}

// NewBackupActionHandler creates the concrete series-copy handler. A nil
// progress channel disables socket-only progress.
func NewBackupActionHandler(
	progress *FileProgressChannel,
	openDestination DestinationOpener,
	capacityRecorder CapacityRecorder,
) (BackupActionHandler, error) {
	if openDestination == nil {
		return nil, errors.New("executor: destination opener is required")
	}
	return func(
		ctx context.Context,
		request BackupActionRequest,
	) error {
		result, err := copySeries(
			ctx,
			request,
			progress,
			openDestination,
			capacityRecorder,
		)
		if err != nil {
			return err
		}
		*request.result = result
		return nil
	}, nil
}

// OpenDestination opens a normal filesystem destination.
func OpenDestination(path string) (Destination, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return nil, err
	}
	return file, nil
}

type archiveFile struct {
	path    string
	source  string
	size    int64
	modTime time.Time
}

type itemFailure struct {
	reason backupplan.ItemReason
	detail string
	err    error
}

func (e *itemFailure) Error() string {
	if e.detail != "" {
		return e.detail
	}
	return e.err.Error()
}

func (e *itemFailure) Unwrap() error {
	return e.err
}

func copySeries(
	ctx context.Context,
	request BackupActionRequest,
	progress *FileProgressChannel,
	openDestination DestinationOpener,
	capacityRecorder CapacityRecorder,
) (result SnapshotResult, retErr error) {
	snapshotDir, err := tapevolume.SnapshotDir(
		request.Cartridge.Root,
		request.Action.MetadataRef,
		request.Action.Generation,
	)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("executor: copy series target: %w", err)
	}
	if err := prepareCopyTarget(
		snapshotDir,
		request.AllowCommittedOverwrite,
	); err != nil {
		return SnapshotResult{}, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if err := os.RemoveAll(snapshotDir); err != nil {
			cleanupErr := fmt.Errorf("executor: remove in-flight snapshot: %w", err)
			if retErr == nil {
				retErr = cleanupErr
			} else {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()

	seriesRoot := filepath.Join(
		request.LibraryRoot,
		filepath.FromSlash(request.Action.RootPath),
	)
	files, err := buildArchiveSet(seriesRoot)
	if err != nil {
		return SnapshotResult{}, err
	}
	capturedAt := time.Now().UTC().Truncate(time.Second)
	manifestFiles, totalSourceBytes, err := copyArchive(
		ctx,
		request,
		snapshotDir,
		files,
		progress,
		openDestination,
		capacityRecorder,
	)
	if err != nil {
		return SnapshotResult{}, err
	}

	if err := checkPostCopyState(seriesRoot, request.Action); err != nil {
		return SnapshotResult{}, err
	}
	manifest := tapemanifest.Manifest{
		MetadataRef: request.Action.MetadataRef,
		RootPath:    request.Action.RootPath,
		Generation:  request.Action.Generation,
		CapturedAt:  capturedAt,
		WrittenBy: tapemanifest.Writer{
			Version: request.WrittenBy.Version,
			Host:    request.WrittenBy.Host,
		},
		TotalBytes: totalSourceBytes,
		Files:      manifestFiles,
	}
	if err := tapemanifest.Write(snapshotDir, manifest); err != nil {
		return SnapshotResult{}, writeFailure("write manifest", err)
	}
	if err := tapemanifest.Commit(snapshotDir); err != nil {
		return SnapshotResult{}, writeFailure("commit manifest", err)
	}
	committed = true
	manifestBytes, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.json"))
	if err != nil {
		return SnapshotResult{}, writeFailure("read committed manifest", err)
	}
	completeBytes, err := os.ReadFile(filepath.Join(snapshotDir, "complete.json"))
	if err != nil {
		return SnapshotResult{}, writeFailure("read completion marker", err)
	}
	return SnapshotResult{
		MetadataRef: request.Action.MetadataRef,
		Generation:  request.Action.Generation,
		Manifest:    manifestBytes,
		Complete:    completeBytes,
	}, nil
}

func copyArchive(
	ctx context.Context,
	request BackupActionRequest,
	snapshotDir string,
	files []archiveFile,
	progress *FileProgressChannel,
	openDestination DestinationOpener,
	capacityRecorder CapacityRecorder,
) ([]tapemanifest.File, int64, error) {
	if err := os.MkdirAll(filepath.Join(snapshotDir, "tree"), 0o775); err != nil {
		return nil, 0, writeFailure("create snapshot tree", err)
	}
	if err := appendSessionEvent(request.Journal, backupplan.Event{
		Type:        backupplan.EventItemStarted,
		PlanID:      request.PlanID,
		MetadataRef: request.Action.MetadataRef,
		Generation:  request.Action.Generation,
		Bytes:       request.Action.Bytes,
		Files:       len(files),
	}); err != nil {
		return nil, 0, err
	}

	manifestFiles := make([]tapemanifest.File, 0, len(files))
	var totalSourceBytes int64
	var itemBytesWritten int64
	for _, file := range files {
		totalSourceBytes += file.size
		manifestFile, written, err := copyArchiveFile(
			ctx,
			request,
			snapshotDir,
			file,
			itemBytesWritten,
			progress,
			openDestination,
			capacityRecorder,
		)
		itemBytesWritten += written
		if err != nil {
			return nil, 0, err
		}
		manifestFiles = append(manifestFiles, manifestFile)
	}
	return manifestFiles, totalSourceBytes, nil
}

func copyArchiveFile(
	ctx context.Context,
	request BackupActionRequest,
	snapshotDir string,
	file archiveFile,
	itemBytesWritten int64,
	progress *FileProgressChannel,
	openDestination DestinationOpener,
	capacityRecorder CapacityRecorder,
) (tapemanifest.File, int64, error) {
	if err := ctx.Err(); err != nil {
		return tapemanifest.File{}, 0, err
	}
	if err := appendSessionEvent(request.Journal, backupplan.Event{
		Type:        backupplan.EventFileStarted,
		PlanID:      request.PlanID,
		MetadataRef: request.Action.MetadataRef,
		Path:        file.path,
		Bytes:       file.size,
	}); err != nil {
		return tapemanifest.File{}, 0, err
	}

	destination := filepath.Join(
		snapshotDir,
		"tree",
		filepath.FromSlash(file.path),
	)
	if err := os.MkdirAll(filepath.Dir(destination), 0o775); err != nil {
		return tapemanifest.File{}, 0, writeFailure(
			"create destination directory",
			err,
		)
	}
	hash, written, err := copyFile(
		ctx,
		request,
		file,
		destination,
		itemBytesWritten,
		progress,
		openDestination,
		capacityRecorder,
	)
	if err != nil {
		return tapemanifest.File{}, written, err
	}
	if err := os.Chtimes(destination, file.modTime, file.modTime); err != nil {
		return tapemanifest.File{}, written, writeFailure(
			"preserve destination mtime",
			err,
		)
	}
	if err := appendSessionEvent(request.Journal, backupplan.Event{
		Type:        backupplan.EventFileCompleted,
		PlanID:      request.PlanID,
		MetadataRef: request.Action.MetadataRef,
		Path:        file.path,
	}); err != nil {
		return tapemanifest.File{}, written, err
	}
	return tapemanifest.File{
		Path:    file.path,
		Size:    file.size,
		ModTime: file.modTime.UTC().Truncate(time.Second),
		Hash:    hash,
	}, written, nil
}

func prepareCopyTarget(
	snapshotDir string,
	allowCommittedOverwrite bool,
) error {
	_, err := os.Lstat(snapshotDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return writeFailure("inspect copy target", err)
	}
	if _, err := tapemanifest.Read(snapshotDir); err == nil &&
		!allowCommittedOverwrite {
		return fmt.Errorf(
			"executor: copy target %q is already committed",
			snapshotDir,
		)
	}
	if err := os.RemoveAll(snapshotDir); err != nil {
		return writeFailure("remove uncommitted copy target", err)
	}
	return nil
}

func buildArchiveSet(seriesRoot string) ([]archiveFile, error) {
	files := make([]archiveFile, 0)
	foundSeriesMetadata := false
	err := filepath.WalkDir(
		seriesRoot,
		func(filePath string, entry fs.DirEntry, walkErr error) error {
			return collectArchiveEntry(
				seriesRoot,
				filePath,
				entry,
				walkErr,
				&files,
				&foundSeriesMetadata,
			)
		},
	)
	if err != nil {
		return nil, err
	}
	if !foundSeriesMetadata {
		return nil, &itemFailure{
			reason: backupplan.ReasonSeriesMoved,
			detail: "executor: .kura/series.json is missing from archive set",
		}
	}
	slices.SortFunc(files, func(a, b archiveFile) int {
		return strings.Compare(a.path, b.path)
	})
	return files, nil
}

func collectArchiveEntry(
	seriesRoot, filePath string,
	entry fs.DirEntry,
	walkErr error,
	files *[]archiveFile,
	foundSeriesMetadata *bool,
) error {
	if walkErr != nil {
		return payloadFailure("walk archive set", walkErr)
	}
	if filePath == seriesRoot {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return payloadFailure(
				"inspect series root",
				errors.New("series root is not a real directory"),
			)
		}
		return nil
	}
	relative, err := filepath.Rel(seriesRoot, filePath)
	if err != nil {
		return payloadFailure("resolve archive path", err)
	}
	relative = filepath.ToSlash(relative)
	include, err := includeArchiveEntry(relative, entry)
	if err != nil || !include {
		return err
	}
	info, err := entry.Info()
	if err != nil {
		return payloadFailure("stat archive file", err)
	}
	if !info.Mode().IsRegular() {
		return unsupportedFailure(relative)
	}
	if !norm.NFC.IsNormalString(relative) {
		return unsupportedFailure(relative)
	}
	*files = append(*files, archiveFile{
		path:    relative,
		source:  filePath,
		size:    info.Size(),
		modTime: info.ModTime(),
	})
	if relative == ".kura/series.json" {
		*foundSeriesMetadata = true
	}
	return nil
}

func includeArchiveEntry(relative string, entry fs.DirEntry) (bool, error) {
	if relative == ".kura" {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return false, unsupportedFailure(relative)
		}
		return false, nil
	}
	if strings.HasPrefix(relative, ".kura/") &&
		relative != ".kura/series.json" {
		if entry.IsDir() {
			return false, filepath.SkipDir
		}
		return false, nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return false, unsupportedFailure(relative)
	}
	if entry.IsDir() {
		return false, nil
	}
	return true, nil
}

func copyFile(
	ctx context.Context,
	request BackupActionRequest,
	file archiveFile,
	destination string,
	itemBytesWritten int64,
	progress *FileProgressChannel,
	openDestination DestinationOpener,
	capacityRecorder CapacityRecorder,
) (hash string, written int64, retErr error) {
	source, err := os.Open(file.source)
	if err != nil {
		return "", 0, payloadFailure("open source", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			closeErr := payloadFailure("close source", err)
			if retErr == nil {
				retErr = closeErr
			} else {
				retErr = errors.Join(retErr, closeErr)
			}
		}
	}()

	target, err := openDestination(destination)
	if err != nil {
		return "", 0, writeFailure("open destination", err)
	}
	startedAt := time.Now()
	hasher := sha256.New()
	writer := &progressWriter{
		ctx:              ctx,
		destination:      target,
		progress:         progress,
		capacityRecorder: capacityRecorder,
		request:          request,
		file:             file,
		itemBytesWritten: itemBytesWritten,
		startedAt:        startedAt,
	}
	written, copyErr := io.Copy(
		writer,
		io.TeeReader(&contextReader{ctx: ctx, reader: source}, hasher),
	)

	syncErr := target.Sync()
	closeErr := target.Close()
	var countErr error
	if written != file.size {
		countErr = fmt.Errorf(
			"wrote %d bytes, want source size %d",
			written,
			file.size,
		)
	}
	if barrierErr := errors.Join(copyErr, syncErr, closeErr, countErr); barrierErr != nil {
		if errors.Is(barrierErr, context.Canceled) {
			return "", written, context.Canceled
		}
		return "", written, writeFailure("durability barrier", barrierErr)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), written, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type progressWriter struct {
	ctx              context.Context
	destination      io.Writer
	progress         *FileProgressChannel
	capacityRecorder CapacityRecorder
	request          BackupActionRequest
	file             archiveFile
	itemBytesWritten int64
	fileBytesWritten int64
	startedAt        time.Time
}

func (w *progressWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.destination.Write(data)
	if n > 0 {
		w.fileBytesWritten += int64(n)
		if w.capacityRecorder != nil {
			if recordErr := w.capacityRecorder.RecordWrite(
				w.request.Cartridge.TapeID,
				int64(n),
			); recordErr != nil {
				return n, recordErr
			}
		}
		now := time.Now()
		elapsed := now.Sub(w.startedAt).Seconds()
		var rate float64
		if elapsed > 0 {
			rate = float64(w.fileBytesWritten) / elapsed
		}
		w.progress.Offer(FileProgress{
			PlanID:           w.request.PlanID,
			MetadataRef:      w.request.Action.MetadataRef,
			Path:             w.file.path,
			FileBytesWritten: w.fileBytesWritten,
			FileBytesTotal:   w.file.size,
			ItemBytesWritten: w.itemBytesWritten + w.fileBytesWritten,
			ItemBytesTotal:   w.request.Action.Bytes,
			BytesPerSecond:   rate,
			At:               now.UTC(),
		})
	}
	return n, err
}

func checkPostCopyState(seriesRoot string, action backupplan.Action) error {
	actual, err := fingerprint.ComputeExcludingKura(seriesRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &itemFailure{
				reason: backupplan.ReasonSeriesRootMissing,
				detail: fmt.Sprintf(
					"executor: re-walk live series: %v",
					err,
				),
				err: err,
			}
		}
		if errors.Is(err, fingerprint.ErrSymlink) ||
			errors.Is(err, fingerprint.ErrNonRegularFile) {
			return unsupportedFailure(err.Error())
		}
		return payloadFailure("re-walk live series", err)
	}
	if string(actual) != action.PayloadFingerprint {
		return &itemFailure{
			reason: backupplan.ReasonPayloadDrift,
			detail: fmt.Sprintf(
				"executor: live payload fingerprint is %q, want %q",
				actual,
				action.PayloadFingerprint,
			),
		}
	}
	metadata, err := seriesmeta.Read(filepath.Join(seriesRoot, ".kura", "series.json"))
	if err != nil {
		return &itemFailure{
			reason: backupplan.ReasonSeriesMoved,
			detail: fmt.Sprintf("executor: re-read series metadata: %v", err),
			err:    err,
		}
	}
	if metadata.MetadataRef != action.MetadataRef {
		return &itemFailure{
			reason: backupplan.ReasonSeriesMoved,
			detail: fmt.Sprintf(
				"executor: metadataRef is %q, want %q",
				metadata.MetadataRef,
				action.MetadataRef,
			),
		}
	}
	if metadata.Generation != action.Generation {
		return &itemFailure{
			reason: backupplan.ReasonSeriesMoved,
			detail: fmt.Sprintf(
				"executor: generation is %d, want %d",
				metadata.Generation,
				action.Generation,
			),
		}
	}
	if metadata.HasStagedIntent {
		return &itemFailure{
			reason: backupplan.ReasonStagedIntent,
			detail: "executor: series has staged intent",
		}
	}
	if metadata.HasActiveClaim {
		return &itemFailure{
			reason: backupplan.ReasonClaimStale,
			detail: "executor: series has an active claim",
		}
	}
	return nil
}

func writeFailure(operation string, err error) error {
	return &itemFailure{
		reason: backupplan.ReasonWriteFailed,
		detail: fmt.Sprintf("executor: %s: %v", operation, err),
		err:    err,
	}
}

func payloadFailure(operation string, err error) error {
	return &itemFailure{
		reason: backupplan.ReasonPayloadDrift,
		detail: fmt.Sprintf("executor: %s: %v", operation, err),
		err:    err,
	}
}

func unsupportedFailure(detail string) error {
	err := errors.New(detail)
	return &itemFailure{
		reason: backupplan.ReasonUnsupportedFileType,
		detail: "executor: unsupported archive entry " + detail,
		err:    err,
	}
}
