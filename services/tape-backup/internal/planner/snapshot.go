package planner

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/wyvernzora/kura/services/tape-backup/internal/fingerprint"
	"github.com/wyvernzora/kura/services/tape-backup/internal/seriesmeta"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapecatalog"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapemanifest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"golang.org/x/text/unicode/norm"
)

var errNonNFCPath = errors.New("planner: non-nfc path")

// ReadLibrarySnapshot walks direct tracked children of libraryRoot.
func ReadLibrarySnapshot(libraryRoot string, freeSpaceMargin int64) (LibrarySnapshot, error) {
	if freeSpaceMargin < 0 {
		return LibrarySnapshot{}, errors.New("planner: free space margin must not be negative")
	}
	entries, err := os.ReadDir(libraryRoot)
	if err != nil {
		return LibrarySnapshot{}, fmt.Errorf("planner: read library root: %w", err)
	}

	snapshot := LibrarySnapshot{Series: make([]Series, 0, len(entries))}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		rootPath := entry.Name()
		seriesRoot := filepath.Join(libraryRoot, rootPath)
		metadataPath := filepath.Join(seriesRoot, ".kura", "series.json")
		if _, err := os.Stat(metadataPath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return LibrarySnapshot{}, fmt.Errorf(
				"planner: inspect series metadata %q: %w",
				rootPath,
				err,
			)
		}

		series, err := readSeries(seriesRoot, rootPath, metadataPath, freeSpaceMargin)
		if err != nil {
			return LibrarySnapshot{}, err
		}
		snapshot.Series = append(snapshot.Series, series)
	}
	sortSeriesByIdentity(snapshot.Series)
	return snapshot, nil
}

func readSeries(
	seriesRoot, rootPath, metadataPath string,
	freeSpaceMargin int64,
) (Series, error) {
	metadata, err := seriesmeta.Read(metadataPath)
	if err != nil {
		return Series{}, fmt.Errorf("planner: read series %q metadata: %w", rootPath, err)
	}
	series := Series{
		MetadataRef: metadata.MetadataRef,
		RootPath:    rootPath,
		Generation:  metadata.Generation,
		Eligibility: EligibilityEligible,
	}
	if metadata.HasStagedIntent {
		series.Eligibility = EligibilityDeferred
		series.Reason = ReasonStagedIntent
		return series, nil
	}
	if metadata.HasActiveClaim {
		series.Eligibility = EligibilityDeferred
		series.Reason = ReasonActiveClaim
		return series, nil
	}

	digest, err := fingerprint.ComputeExcludingKura(seriesRoot)
	if err != nil {
		if structuralReason, ok := fingerprintStructuralReason(err); ok {
			series.Eligibility = EligibilityUnbackupable
			series.Reason = structuralReason
			series.Detail = err.Error()
			return series, nil
		}
		return Series{}, fmt.Errorf("planner: fingerprint series %q: %w", rootPath, err)
	}
	series.PayloadFingerprint = string(digest)

	bytes, err := archiveBytes(seriesRoot)
	if err != nil {
		if structuralReason, ok := archiveStructuralReason(err); ok {
			series.Eligibility = EligibilityUnbackupable
			series.Reason = structuralReason
			series.Detail = err.Error()
			return series, nil
		}
		return Series{}, fmt.Errorf("planner: size series %q: %w", rootPath, err)
	}
	series.Bytes = bytes

	if err := validateBackupIdentity(series); err != nil {
		series.Eligibility = EligibilityUnbackupable
		series.Reason = ReasonInvalidAction
		series.Detail = err.Error()
		return series, nil
	}
	if bytes > largestNominalCapacity()-freeSpaceMargin {
		series.Eligibility = EligibilityUnbackupable
		series.Reason = ReasonTooLarge
		series.Detail = fmt.Sprintf(
			"series needs %d bytes plus %d-byte margin; largest cartridge is %d bytes",
			bytes,
			freeSpaceMargin,
			largestNominalCapacity(),
		)
	}
	return series, nil
}

func fingerprintStructuralReason(err error) (Reason, bool) {
	switch {
	case errors.Is(err, fingerprint.ErrSymlink):
		return ReasonSymlink, true
	case errors.Is(err, fingerprint.ErrNonRegularFile):
		return ReasonNonRegular, true
	default:
		return "", false
	}
}

func archiveStructuralReason(err error) (Reason, bool) {
	switch {
	case errors.Is(err, errNonNFCPath):
		return ReasonNonNFCPath, true
	case errors.Is(err, fingerprint.ErrSymlink):
		return ReasonSymlink, true
	case errors.Is(err, fingerprint.ErrNonRegularFile):
		return ReasonNonRegular, true
	default:
		return "", false
	}
}

func archiveBytes(seriesRoot string) (int64, error) {
	var total int64
	err := filepath.WalkDir(seriesRoot, func(
		filePath string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		size, err := archiveEntryBytes(seriesRoot, filePath, entry)
		if err != nil {
			return err
		}
		if size > math.MaxInt64-total {
			return errors.New("archive byte total overflows int64")
		}
		total += size
		return nil
	})
	return total, err
}

func archiveEntryBytes(seriesRoot, filePath string, entry fs.DirEntry) (int64, error) {
	if filePath == seriesRoot {
		return 0, nil
	}
	relative, err := filepath.Rel(seriesRoot, filePath)
	if err != nil {
		return 0, err
	}
	relative = filepath.ToSlash(relative)
	if relative == ".kura" {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return 0, fmt.Errorf("%w: %q", fingerprint.ErrNonRegularFile, relative)
		}
		return 0, nil
	}
	if strings.HasPrefix(relative, ".kura/") &&
		relative != ".kura/series.json" {
		if entry.IsDir() {
			return 0, filepath.SkipDir
		}
		return 0, nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("%w: %q", fingerprint.ErrSymlink, relative)
	}
	if entry.IsDir() {
		return 0, nil
	}
	info, err := entry.Info()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %q", fingerprint.ErrNonRegularFile, relative)
	}
	if !norm.NFC.IsNormalString(relative) {
		return 0, fmt.Errorf("%w: %q", errNonNFCPath, relative)
	}
	return info.Size(), nil
}

func validateBackupIdentity(series Series) error {
	if _, err := snapshotname.Format(series.MetadataRef, series.Generation); err != nil {
		return err
	}
	if series.RootPath == "" {
		return errors.New("root path is required")
	}
	if !utf8.ValidString(series.RootPath) {
		return fmt.Errorf("root path %q is not valid UTF-8", series.RootPath)
	}
	if strings.ContainsAny(series.RootPath, "\x00\n") ||
		path.Clean(series.RootPath) != series.RootPath ||
		path.IsAbs(series.RootPath) ||
		!norm.NFC.IsNormalString(series.RootPath) {
		return fmt.Errorf("root path %q cannot form a backup action", series.RootPath)
	}
	return nil
}

// ReadCatalogSnapshot reads active volumes, observations, and committed
// manifests from the catalog mirror.
func ReadCatalogSnapshot(stateRoot string) (CatalogSnapshot, error) {
	ids, err := tapecatalog.ListActive(stateRoot)
	if err != nil {
		return CatalogSnapshot{}, fmt.Errorf("planner: list active catalog: %w", err)
	}
	snapshot := CatalogSnapshot{Volumes: make([]Volume, 0, len(ids))}
	for _, id := range ids {
		observed, err := tapecatalog.LoadObserved(stateRoot, id)
		if err != nil {
			return CatalogSnapshot{}, fmt.Errorf(
				"planner: load observation for volume %s: %w",
				id,
				err,
			)
		}
		volumeDir, err := tapecatalog.VolumeDir(stateRoot, id)
		if err != nil {
			return CatalogSnapshot{}, fmt.Errorf(
				"planner: locate volume %s: %w",
				id,
				err,
			)
		}
		committed, err := readCommittedSnapshots(volumeDir)
		if err != nil {
			return CatalogSnapshot{}, fmt.Errorf(
				"planner: read volume %s snapshots: %w",
				id,
				err,
			)
		}
		snapshot.Volumes = append(snapshot.Volumes, Volume{
			VolumeID:      id,
			TapeID:        observed.TapeID,
			CapacityBytes: observed.CapacityBytes,
			FreeBytes:     observed.FreeBytes,
			Snapshots:     committed,
		})
	}
	sortVolumes(snapshot.Volumes)
	return snapshot, nil
}

func readCommittedSnapshots(volumeDir string) ([]Snapshot, error) {
	snapshotsDir := tapevolume.SnapshotsDir(volumeDir)
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return nil, err
	}
	snapshots := make([]Snapshot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := tapemanifest.Read(filepath.Join(snapshotsDir, entry.Name()))
		if errors.Is(err, tapemanifest.ErrIncomplete) {
			continue
		}
		if err != nil {
			return nil, err
		}
		fingerprintEntries := make([]fingerprint.Entry, 0, len(manifest.Files))
		for _, file := range manifest.Files {
			fingerprintEntries = append(fingerprintEntries, fingerprint.Entry{
				Path:    file.Path,
				Size:    file.Size,
				ModTime: file.ModTime,
			})
		}
		digest, err := fingerprint.OfExcludingKura(fingerprintEntries)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, Snapshot{
			MetadataRef:        manifest.MetadataRef,
			Generation:         manifest.Generation,
			TotalBytes:         manifest.TotalBytes,
			PayloadFingerprint: string(digest),
		})
	}
	slices.SortFunc(snapshots, compareSnapshots)
	return snapshots, nil
}

func sortSeriesByIdentity(series []Series) {
	slices.SortFunc(series, compareSeriesIdentity)
}

func compareSeriesIdentity(a, b Series) int {
	if result := strings.Compare(a.MetadataRef, b.MetadataRef); result != 0 {
		return result
	}
	if a.Generation != b.Generation {
		return a.Generation - b.Generation
	}
	return strings.Compare(a.RootPath, b.RootPath)
}

func compareSnapshots(a, b Snapshot) int {
	if result := strings.Compare(a.MetadataRef, b.MetadataRef); result != 0 {
		return result
	}
	return a.Generation - b.Generation
}

func sortVolumes(volumes []Volume) {
	slices.SortFunc(volumes, func(a, b Volume) int {
		return strings.Compare(string(a.VolumeID), string(b.VolumeID))
	})
	for i := range volumes {
		slices.SortFunc(volumes[i].Snapshots, compareSnapshots)
	}
}
