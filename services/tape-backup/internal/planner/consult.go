package planner

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

// Consult computes the pending set, allocation roster, and R19 sizing report.
func Consult(
	library LibrarySnapshot,
	catalog CatalogSnapshot,
	blanks []Blank,
	freeSpaceMargin int64,
) (Report, error) {
	if freeSpaceMargin < 0 {
		return Report{}, errors.New("planner: free space margin must not be negative")
	}
	library = cloneLibrarySnapshot(library)
	catalog = cloneCatalogSnapshot(catalog)
	blanks = slices.Clone(blanks)
	sortSeriesByIdentity(library.Series)
	sortVolumes(catalog.Volumes)
	slices.SortFunc(blanks, func(a, b Blank) int {
		return strings.Compare(string(a.TapeID), string(b.TapeID))
	})
	if err := validateInputs(library, catalog, blanks); err != nil {
		return Report{}, err
	}

	report := Report{
		Pending:                   []Series{},
		Assignments:               []Assignment{},
		Shortfall:                 []Series{},
		Deferred:                  []Series{},
		Unbackupable:              []Series{},
		LineageRefusals:           []LineageRefusal{},
		InitPlansAwaitingApproval: []InitPlanAwaitingApproval{},
	}
	committed, maxGeneration := catalogFacts(catalog)
	for _, series := range library.Series {
		pair := Pair{MetadataRef: series.MetadataRef, Generation: series.Generation}
		if series.Generation < maxGeneration[series.MetadataRef] {
			report.LineageRefusals = append(report.LineageRefusals, LineageRefusal{
				Series:                 series,
				CatalogedMaxGeneration: maxGeneration[series.MetadataRef],
			})
			continue
		}
		if _, exists := committed[pair]; exists {
			continue
		}
		switch series.Eligibility {
		case EligibilityEligible:
			report.Pending = append(report.Pending, series)
		case EligibilityDeferred:
			report.Deferred = append(report.Deferred, series)
		case EligibilityUnbackupable:
			report.Unbackupable = append(report.Unbackupable, series)
		default:
			return Report{}, fmt.Errorf(
				"planner: series %q has unsupported eligibility %q",
				series.MetadataRef,
				series.Eligibility,
			)
		}
	}

	assignments, shortfall := allocate(
		report.Pending,
		catalog.Volumes,
		blanks,
		freeSpaceMargin,
	)
	report.Assignments = assignments
	report.Shortfall = shortfall
	report.Sizing = sizeReport(
		report.Pending,
		assignments,
		shortfall,
		catalog.Volumes,
		blanks,
		freeSpaceMargin,
	)
	return report, nil
}

// ConsultTarget computes the pending set and assigns only to target. Catalog
// protection is still evaluated globally.
func ConsultTarget(
	library LibrarySnapshot,
	catalog CatalogSnapshot,
	target Target,
	freeSpaceMargin int64,
) (Report, error) {
	report, err := Consult(library, catalog, nil, freeSpaceMargin)
	if err != nil {
		return Report{}, err
	}

	var volumes []Volume
	var blanks []Blank
	switch target.Kind {
	case TargetKnown:
		for _, candidate := range catalog.Volumes {
			if candidate.VolumeID == target.VolumeID &&
				candidate.TapeID == target.TapeID {
				volumes = []Volume{candidate}
				break
			}
		}
		if len(volumes) == 0 {
			return Report{}, fmt.Errorf(
				"planner: target volume %s on tape %s is not in the catalog",
				target.VolumeID,
				target.TapeID,
			)
		}
	case TargetBlank:
		if _, err := tape.ParseID(string(target.TapeID)); err != nil {
			return Report{}, fmt.Errorf("planner: target blank: %w", err)
		}
		for _, candidate := range catalog.Volumes {
			if candidate.TapeID == target.TapeID {
				return Report{}, fmt.Errorf(
					"planner: target blank tape %s is known as volume %s",
					target.TapeID,
					candidate.VolumeID,
				)
			}
		}
		blanks = []Blank{{TapeID: target.TapeID}}
	default:
		return Report{}, fmt.Errorf("planner: unsupported target kind %q", target.Kind)
	}

	report.Assignments, report.Shortfall = allocate(
		report.Pending,
		volumes,
		blanks,
		freeSpaceMargin,
	)
	report.Sizing = sizeReport(
		report.Pending,
		report.Assignments,
		report.Shortfall,
		volumes,
		blanks,
		freeSpaceMargin,
	)
	return report, nil
}

func validateInputs(
	library LibrarySnapshot,
	catalog CatalogSnapshot,
	blanks []Blank,
) error {
	if err := validateLibraryInputs(library); err != nil {
		return err
	}
	knownTapes, err := validateCatalogInputs(catalog)
	if err != nil {
		return err
	}
	return validateBlankInputs(blanks, knownTapes)
}

func validateLibraryInputs(library LibrarySnapshot) error {
	refs := make(map[string]string, len(library.Series))
	pairs := make(map[Pair]struct{}, len(library.Series))
	for _, series := range library.Series {
		if series.Bytes < 0 {
			return fmt.Errorf("planner: series %q bytes must not be negative", series.MetadataRef)
		}
		if root, exists := refs[series.MetadataRef]; exists && root != series.RootPath {
			return fmt.Errorf(
				"planner: metadataRef %q appears at roots %q and %q",
				series.MetadataRef,
				root,
				series.RootPath,
			)
		}
		refs[series.MetadataRef] = series.RootPath
		pair := Pair{MetadataRef: series.MetadataRef, Generation: series.Generation}
		if _, exists := pairs[pair]; exists {
			return fmt.Errorf(
				"planner: duplicate library pair (%q, %d)",
				series.MetadataRef,
				series.Generation,
			)
		}
		pairs[pair] = struct{}{}
	}
	return nil
}

func validateCatalogInputs(catalog CatalogSnapshot) (map[tape.ID]volume.ID, error) {
	volumeIDs := make(map[volume.ID]struct{}, len(catalog.Volumes))
	knownTapes := make(map[tape.ID]volume.ID, len(catalog.Volumes))
	for _, catalogVolume := range catalog.Volumes {
		if _, err := volume.ParseID(string(catalogVolume.VolumeID)); err != nil {
			return nil, fmt.Errorf("planner: catalog volume: %w", err)
		}
		if _, err := tape.ParseID(string(catalogVolume.TapeID)); err != nil {
			return nil, fmt.Errorf(
				"planner: catalog volume %s: %w",
				catalogVolume.VolumeID,
				err,
			)
		}
		if catalogVolume.FreeBytes < 0 ||
			catalogVolume.FreeBytes > catalogVolume.CapacityBytes {
			return nil, fmt.Errorf(
				"planner: volume %s has invalid capacity %d/%d",
				catalogVolume.VolumeID,
				catalogVolume.FreeBytes,
				catalogVolume.CapacityBytes,
			)
		}
		if _, exists := volumeIDs[catalogVolume.VolumeID]; exists {
			return nil, fmt.Errorf("planner: duplicate volume %s", catalogVolume.VolumeID)
		}
		volumeIDs[catalogVolume.VolumeID] = struct{}{}
		if other, exists := knownTapes[catalogVolume.TapeID]; exists {
			return nil, fmt.Errorf(
				"planner: tape %s labels volumes %s and %s",
				catalogVolume.TapeID,
				other,
				catalogVolume.VolumeID,
			)
		}
		knownTapes[catalogVolume.TapeID] = catalogVolume.VolumeID
	}
	return knownTapes, nil
}

func validateBlankInputs(blanks []Blank, knownTapes map[tape.ID]volume.ID) error {
	declared := make(map[tape.ID]struct{}, len(blanks))
	for _, blank := range blanks {
		if _, err := tape.ParseID(string(blank.TapeID)); err != nil {
			return fmt.Errorf("planner: declared blank: %w", err)
		}
		if knownVolume, exists := knownTapes[blank.TapeID]; exists {
			return fmt.Errorf(
				"planner: declared blank tape %s is known as volume %s",
				blank.TapeID,
				knownVolume,
			)
		}
		if _, exists := declared[blank.TapeID]; exists {
			return fmt.Errorf("planner: duplicate declared blank tape %s", blank.TapeID)
		}
		declared[blank.TapeID] = struct{}{}
	}
	return nil
}

func catalogFacts(
	catalog CatalogSnapshot,
) (committed map[Pair]struct{}, maxGeneration map[string]int) {
	committed = make(map[Pair]struct{})
	maxGeneration = make(map[string]int)
	for _, catalogVolume := range catalog.Volumes {
		for _, snapshot := range catalogVolume.Snapshots {
			pair := Pair{
				MetadataRef: snapshot.MetadataRef,
				Generation:  snapshot.Generation,
			}
			committed[pair] = struct{}{}
			if snapshot.Generation > maxGeneration[snapshot.MetadataRef] {
				maxGeneration[snapshot.MetadataRef] = snapshot.Generation
			}
		}
	}
	return committed, maxGeneration
}

type targetBudget struct {
	target    Target
	remaining int64
	series    []Series
	bytes     int64
}

func allocate(
	pending []Series,
	volumes []Volume,
	blanks []Blank,
	freeSpaceMargin int64,
) ([]Assignment, []Series) {
	ordered := slices.Clone(pending)
	slices.SortFunc(ordered, compareSeriesLargestFirst)
	known := knownTargetBudgets(volumes, freeSpaceMargin)
	blankTargets := blankTargetBudgets(blanks, freeSpaceMargin)
	unassigned := make([]Series, 0, len(ordered))

	for _, series := range ordered {
		target := incumbentTarget(series.MetadataRef, volumes, known, series.Bytes)
		if target < 0 {
			unassigned = append(unassigned, series)
			continue
		}
		assignTo(&known[target], series)
	}
	unassigned = firstFit(unassigned, known)
	shortfall := firstFit(unassigned, blankTargets)

	assignments := make([]Assignment, 0, len(known)+len(blankTargets))
	assignments = appendAssignments(assignments, known)
	assignments = appendAssignments(assignments, blankTargets)
	return assignments, shortfall
}

func knownTargetBudgets(volumes []Volume, margin int64) []targetBudget {
	targets := make([]targetBudget, 0, len(volumes))
	for _, catalogVolume := range volumes {
		targets = append(targets, targetBudget{
			target: Target{
				Kind:          TargetKnown,
				VolumeID:      catalogVolume.VolumeID,
				TapeID:        catalogVolume.TapeID,
				CapacityBytes: catalogVolume.CapacityBytes,
				FreeBytes:     catalogVolume.FreeBytes,
			},
			remaining: usableBytes(catalogVolume.FreeBytes, margin),
			series:    []Series{},
		})
	}
	return targets
}

func blankTargetBudgets(blanks []Blank, margin int64) []targetBudget {
	targets := make([]targetBudget, 0, len(blanks))
	for _, blank := range blanks {
		capacity, _ := NominalCapacity(blank.TapeID)
		targets = append(targets, targetBudget{
			target: Target{
				Kind:          TargetBlank,
				TapeID:        blank.TapeID,
				CapacityBytes: capacity,
				FreeBytes:     capacity,
			},
			remaining: usableBytes(capacity, margin),
			series:    []Series{},
		})
	}
	return targets
}

func incumbentTarget(
	metadataRef string,
	volumes []Volume,
	targets []targetBudget,
	bytes int64,
) int {
	for i, catalogVolume := range volumes {
		if targets[i].remaining < bytes {
			continue
		}
		for _, snapshot := range catalogVolume.Snapshots {
			if snapshot.MetadataRef == metadataRef {
				return i
			}
		}
	}
	return -1
}

func firstFit(series []Series, targets []targetBudget) []Series {
	unassigned := make([]Series, 0, len(series))
	for _, item := range series {
		target := -1
		for i := range targets {
			if targets[i].remaining >= item.Bytes {
				target = i
				break
			}
		}
		if target < 0 {
			unassigned = append(unassigned, item)
			continue
		}
		assignTo(&targets[target], item)
	}
	return unassigned
}

func assignTo(target *targetBudget, series Series) {
	target.series = append(target.series, series)
	target.bytes += series.Bytes
	target.remaining -= series.Bytes
}

func appendAssignments(assignments []Assignment, targets []targetBudget) []Assignment {
	for _, target := range targets {
		if len(target.series) == 0 {
			continue
		}
		assignments = append(assignments, Assignment{
			Target: target.target,
			Series: slices.Clone(target.series),
			Bytes:  target.bytes,
		})
	}
	return assignments
}

func compareSeriesLargestFirst(a, b Series) int {
	if a.Bytes != b.Bytes {
		if a.Bytes > b.Bytes {
			return -1
		}
		return 1
	}
	return compareSeriesIdentity(a, b)
}

func sizeReport(
	pending []Series,
	assignments []Assignment,
	shortfall []Series,
	volumes []Volume,
	blanks []Blank,
	margin int64,
) Sizing {
	sizing := Sizing{}
	for _, series := range pending {
		sizing.PendingBytes += series.Bytes
	}
	for _, catalogVolume := range volumes {
		sizing.KnownRoomBytes += usableBytes(catalogVolume.FreeBytes, margin)
	}
	for _, blank := range blanks {
		capacity, _ := NominalCapacity(blank.TapeID)
		sizing.DeclaredBlankRoomBytes += usableBytes(capacity, margin)
	}
	for _, assignment := range assignments {
		sizing.RosterBytes += assignment.Bytes
	}
	for _, series := range shortfall {
		sizing.ShortfallBytes += series.Bytes
	}

	capacity, generation := sizingBlank(volumes, blanks)
	sizing.NominalBlankCapacityBytes = capacity
	sizing.NominalBlankUsableBytes = usableBytes(capacity, margin)
	sizing.NominalBlankMediaGeneration = generation
	if sizing.ShortfallBytes > 0 && sizing.NominalBlankUsableBytes > 0 {
		count := (sizing.ShortfallBytes + sizing.NominalBlankUsableBytes - 1) /
			sizing.NominalBlankUsableBytes
		if count > math.MaxInt {
			sizing.BringBlanks = math.MaxInt
		} else {
			sizing.BringBlanks = int(count)
		}
	}
	return sizing
}

func sizingBlank(
	volumes []Volume,
	blanks []Blank,
) (capacity int64, generation string) {
	var selected tape.ID
	for _, blank := range blanks {
		candidate, _ := NominalCapacity(blank.TapeID)
		if candidate > capacity {
			selected = blank.TapeID
			capacity = candidate
		}
	}
	if capacity == 0 {
		for _, catalogVolume := range volumes {
			candidate, err := NominalCapacity(catalogVolume.TapeID)
			if err == nil && candidate > capacity {
				selected = catalogVolume.TapeID
				capacity = candidate
			}
		}
	}
	if capacity == 0 {
		capacity = largestNominalCapacity()
		return capacity, "LTO-10"
	}
	return capacity, selected.MediaGeneration()
}

func usableBytes(bytes, margin int64) int64 {
	if bytes <= margin {
		return 0
	}
	return bytes - margin
}

func cloneLibrarySnapshot(snapshot LibrarySnapshot) LibrarySnapshot {
	return LibrarySnapshot{Series: slices.Clone(snapshot.Series)}
}

func cloneCatalogSnapshot(snapshot CatalogSnapshot) CatalogSnapshot {
	volumes := make([]Volume, len(snapshot.Volumes))
	for i, catalogVolume := range snapshot.Volumes {
		volumes[i] = catalogVolume
		volumes[i].Snapshots = slices.Clone(catalogVolume.Snapshots)
	}
	return CatalogSnapshot{Volumes: volumes}
}
