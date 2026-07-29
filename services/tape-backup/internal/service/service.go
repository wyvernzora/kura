// Package service owns tape-backup verbs and the state mutex that composes
// plan lifecycle operations with the live-session registry.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

const reportSchemaVersion = 1

var (
	// ErrSessionActive reports that the one live-session slot is occupied.
	ErrSessionActive = errors.New("tape service: session is already active")
	// ErrApprovalRequired reports an init draft awaiting operator approval.
	ErrApprovalRequired = errors.New("tape service: init plan is awaiting approval")
	// ErrNoWork reports a targeted fill with no pending snapshots.
	ErrNoWork = errors.New("tape service: no pending snapshots for loaded tape")
)

// Refusal is a normal named v1 refusal.
type Refusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r *Refusal) Error() string {
	return r.Code + ": " + r.Message
}

// PlanRequest selects the targeted backup operation. Operation defaults to
// backup; the other values are the v1 stub contracts.
type PlanRequest struct {
	TapeID    tape.ID `json:"tapeID,omitempty"`
	Operation string  `json:"operation,omitempty"`
}

// PlanResult is the targeted decision-tree result.
type PlanResult struct {
	Classification string          `json:"classification"`
	Plan           backupplan.Plan `json:"plan"`
	Persisted      bool            `json:"persisted"`
	Debris         []string        `json:"debris"`
}

// ConsultResult is the persisted R19 report and R20 draft roster.
type ConsultResult struct {
	SchemaVersion int               `json:"schemaVersion"`
	CreatedAt     time.Time         `json:"createdAt"`
	Report        planner.Report    `json:"report"`
	Drafts        []backupplan.Plan `json:"drafts"`
}

// LivePlan is one registered plan in a running session.
type LivePlan struct {
	PlanID string  `json:"planID"`
	TapeID tape.ID `json:"tapeID"`
}

// LiveSession is the read-only roster view.
type LiveSession struct {
	SessionID string     `json:"sessionID"`
	StartedAt time.Time  `json:"startedAt"`
	Plans     []LivePlan `json:"plans"`
}

// DriveStatus is a one-shot availability observation.
type DriveStatus struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// StatusResult is the only automation-safe polling response. Status never
// drafts, discards, promotes, registers, sweeps, or mints.
type StatusResult struct {
	LiveSessions []LiveSession                      `json:"liveSessions"`
	Drive        DriveStatus                        `json:"drive"`
	InitDrafts   []planner.InitPlanAwaitingApproval `json:"initDrafts"`
	Consult      *ConsultResult                     `json:"consult,omitempty"`
}

// Deps are the explicit process-owned service dependencies.
type Deps struct {
	StateRoot       string
	LibraryRoot     string
	LTFSRoot        string
	Drive           executor.Drive
	Backup          executor.BackupActionHandler
	Creator         backupplan.Creator
	FreeSpaceMargin int64
	FlushCadence    int
	PollInterval    time.Duration
	IdleTimeout     time.Duration
	Now             func() time.Time
	NewID           func() string
}

// Service owns the mutex above backupplan and tapecatalog package locks.
type Service struct {
	mu            sync.Mutex
	deps          Deps
	live          map[string]LiveSession
	beforePromote func()
}

// New creates a service without starting work.
func New(deps Deps) (*Service, error) {
	if deps.StateRoot == "" || deps.LibraryRoot == "" {
		return nil, errors.New("tape service: state root and library root are required")
	}
	if deps.Drive == nil || deps.Backup == nil {
		return nil, errors.New("tape service: drive and backup handler are required")
	}
	if deps.Creator.Version == "" || deps.Creator.Host == "" {
		return nil, errors.New("tape service: creator version and host are required")
	}
	if deps.FreeSpaceMargin < 0 || deps.FlushCadence < 1 ||
		deps.PollInterval <= 0 || deps.IdleTimeout <= 0 {
		return nil, errors.New("tape service: invalid execution policy")
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = func() string { return ulid.Make().String() }
	}
	return &Service{deps: deps, live: make(map[string]LiveSession)}, nil
}

// Status reads the roster, drive, init drafts, and persisted consult report.
func (s *Service) Status() (StatusResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := cloneLiveSessions(s.live)

	drafts, err := backupplan.ListDraft(s.deps.StateRoot)
	if err != nil {
		return StatusResult{}, fmt.Errorf("tape service: list draft plans: %w", err)
	}
	initDrafts := initDraftStatuses(drafts, s.deps.Now())
	report, err := readReport(s.deps.StateRoot)
	if err != nil {
		return StatusResult{}, err
	}
	drive, err := s.driveStatus(len(live) > 0)
	if err != nil {
		return StatusResult{}, err
	}
	return StatusResult{
		LiveSessions: live,
		Drive:        drive,
		InitDrafts:   initDrafts,
		Consult:      report,
	}, nil
}

// Consult performs the freshness sweep and roster minting under the service
// mutex, then atomically persists the advisory report.
func (s *Service) Consult(blanks []planner.Blank) (ConsultResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	drafts, err := backupplan.ListDraft(s.deps.StateRoot)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("tape service: list draft plans: %w", err)
	}
	ready, err := backupplan.ListReady(s.deps.StateRoot)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("tape service: list ready plans: %w", err)
	}

	preserved := make([]backupplan.Plan, 0, len(drafts)+len(ready))
	for _, draft := range drafts {
		if isInitPlan(draft) {
			preserved = append(preserved, draft)
			continue
		}
		if err := backupplan.Discard(s.deps.StateRoot, draft.PlanID); err != nil {
			return ConsultResult{}, fmt.Errorf(
				"tape service: discard stale consult draft %s: %w",
				draft.PlanID,
				err,
			)
		}
	}
	preserved = append(preserved, ready...)

	library, catalog, err := s.snapshots()
	if err != nil {
		return ConsultResult{}, err
	}
	library = withoutClaims(library, preserved)
	catalog = withoutTapes(catalog, preserved)
	report, err := planner.Consult(
		library,
		catalog,
		blanks,
		s.deps.FreeSpaceMargin,
	)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("tape service: consult: %w", err)
	}
	report.InitPlansAwaitingApproval = initDraftStatuses(drafts, s.deps.Now())

	minted := make([]backupplan.Plan, 0, len(report.Assignments))
	for _, assignment := range report.Assignments {
		if assignment.Target.Kind != planner.TargetKnown {
			continue
		}
		plan, err := s.materializeFill(catalog, assignment)
		if err != nil {
			return ConsultResult{}, err
		}
		if err := backupplan.Draft(s.deps.StateRoot, plan); err != nil {
			return ConsultResult{}, fmt.Errorf(
				"tape service: draft consult plan %s: %w",
				plan.PlanID,
				err,
			)
		}
		minted = append(minted, plan)
	}
	result := ConsultResult{
		SchemaVersion: reportSchemaVersion,
		CreatedAt:     s.now(),
		Report:        report,
		Drafts:        minted,
	}
	if err := writeReport(s.deps.StateRoot, result); err != nil {
		return ConsultResult{}, err
	}
	return result, nil
}

// Plan walks the targeted decision tree. Fill previews are ephemeral; init
// previews are persisted in draft state for approval.
func (s *Service) Plan(request PlanRequest) (PlanResult, error) {
	if err := refuseStubOperation(request.Operation); err != nil {
		return PlanResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.live) > 0 {
		return PlanResult{}, ErrSessionActive
	}
	return s.targetPlanLocked(request, true)
}

// Approve promotes an init draft. Pure fills never have an approval window.
func (s *Service) Approve(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := backupplan.ReadDraft(s.deps.StateRoot, planID)
	if err != nil {
		return err
	}
	if !isInitPlan(plan) {
		return errors.New("tape service: only init drafts can be approved")
	}
	return backupplan.Approve(s.deps.StateRoot, planID)
}

// Discard retires a draft or ready plan and releases its claimed pairs by
// removing the plan from the live set.
func (s *Service) Discard(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.live {
		for _, plan := range session.Plans {
			if plan.PlanID == planID {
				return fmt.Errorf("tape service: plan %s belongs to live session %s", planID, session.SessionID)
			}
		}
	}
	return backupplan.Discard(s.deps.StateRoot, planID)
}

// Run validates or freshly drafts, promotes, and registers under one critical
// section. The multi-hour executor call runs after releasing the mutex.
func (s *Service) Run(ctx context.Context, request PlanRequest) error {
	if err := refuseStubOperation(request.Operation); err != nil {
		return err
	}
	s.mu.Lock()
	if len(s.live) > 0 {
		s.mu.Unlock()
		return ErrSessionActive
	}
	inspection, err := s.inspectLoaded(request.TapeID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	plan, err := s.selectRunPlanLocked(inspection)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if s.beforePromote != nil {
		s.beforePromote()
	}
	if _, err := backupplan.ReadReady(s.deps.StateRoot, plan.PlanID); err != nil {
		if err := backupplan.Approve(s.deps.StateRoot, plan.PlanID); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	sessionID := s.deps.NewID()
	journal, err := backupplan.CreateSession(s.deps.StateRoot, backupplan.Header{
		SessionID:  sessionID,
		Executor:   s.deps.Creator,
		StartedAt:  s.now(),
		ReadyPlans: []string{plan.PlanID},
	})
	if err != nil {
		discardErr := backupplan.Discard(s.deps.StateRoot, plan.PlanID)
		s.mu.Unlock()
		return errors.Join(
			fmt.Errorf("tape service: create session: %w", err),
			discardErr,
		)
	}
	s.live[sessionID] = LiveSession{
		SessionID: sessionID,
		StartedAt: s.now(),
		Plans:     []LivePlan{{PlanID: plan.PlanID, TapeID: plan.Target.TapeID}},
	}
	s.mu.Unlock()

	runErr := executor.RunSession(
		ctx,
		s.deps.Drive,
		[]backupplan.Plan{plan},
		journal,
		s.deps.Backup,
		executor.SessionOptions{
			PollInterval:    s.deps.PollInterval,
			IdleTimeout:     s.deps.IdleTimeout,
			StateRoot:       s.deps.StateRoot,
			LibraryRoot:     s.deps.LibraryRoot,
			FreeSpaceMargin: s.deps.FreeSpaceMargin,
			FlushCadence:    s.deps.FlushCadence,
		},
	)
	s.mu.Lock()
	delete(s.live, sessionID)
	s.mu.Unlock()
	if closeErr := journal.Close(); closeErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("tape service: close session journal: %w", closeErr))
	}
	if runErr != nil {
		if _, readyErr := backupplan.ReadReady(s.deps.StateRoot, plan.PlanID); readyErr == nil {
			runErr = errors.Join(runErr, backupplan.Discard(s.deps.StateRoot, plan.PlanID))
		}
	}
	return runErr
}

type inspection struct {
	identity        executor.LoadedIdentity
	catalog         planner.CatalogSnapshot
	library         planner.LibrarySnapshot
	marked          []string
	namespaceAbsent bool
	total           int64
	free            int64
}

func (s *Service) inspectLoaded(requested tape.ID) (
	result inspection,
	resultErr error,
) {
	availability, err := executor.ClassifyDriveAvailability(
		s.deps.Drive,
		executor.DriveAvailabilityOptions{LTFSRoot: s.deps.LTFSRoot},
	)
	if err != nil {
		return inspection{}, err
	}
	if availability.CloseRequired {
		defer func() {
			if err := s.deps.Drive.Close(); err != nil {
				resultErr = errors.Join(
					resultErr,
					fmt.Errorf("tape service: close inspection probe: %w", err),
				)
			}
		}()
	}
	if availability.Reason != executor.DriveReady {
		return inspection{}, &Refusal{
			Code:    string(availability.Reason),
			Message: availability.Message,
		}
	}
	identity, err := s.deps.Drive.LoadedIdentity()
	if err != nil {
		return inspection{}, fmt.Errorf("tape service: read loaded identity: %w", err)
	}
	if identity.State == executor.LoadedIdentityUnidentified && requested == "" {
		return inspection{}, &Refusal{
			Code:    "identity_unconfirmed",
			Message: "unidentified media requires the declared cartridge label",
		}
	}
	if identity.State == executor.LoadedIdentityUnidentified {
		if _, err := tape.ParseID(string(requested)); err != nil {
			return inspection{}, &Refusal{Code: "identity_unconfirmed", Message: err.Error()}
		}
		identity.Cartridge.TapeID = requested
	}
	total, free, err := s.deps.Drive.Capacity()
	if errors.Is(err, executor.ErrNotMounted) &&
		identity.State == executor.LoadedIdentityUnidentified {
		total, err = planner.NominalCapacity(identity.Cartridge.TapeID)
		free = total
	}
	if err != nil {
		return inspection{}, fmt.Errorf("tape service: inspect loaded capacity: %w", err)
	}
	library, catalog, err := s.snapshots()
	if err != nil {
		return inspection{}, err
	}
	marked, absent, err := markedSnapshots(identity.Cartridge.Root)
	if err != nil {
		return inspection{}, err
	}
	return inspection{
		identity: identity, catalog: catalog, library: library,
		marked: marked, namespaceAbsent: absent, total: total, free: free,
	}, nil
}

func (s *Service) targetPlanLocked(request PlanRequest, persistInit bool) (PlanResult, error) {
	inspection, err := s.inspectLoaded(request.TapeID)
	if err != nil {
		return PlanResult{}, err
	}
	if err := s.discardTargetedSuperseded(inspection.identity.Cartridge.TapeID); err != nil {
		return PlanResult{}, err
	}
	return s.planFromInspection(inspection, persistInit)
}

func (s *Service) planFromInspection(in inspection, persistInit bool) (PlanResult, error) {
	if in.identity.State == executor.LoadedIdentityUnidentified {
		return s.initPlan(in, persistInit)
	}
	catalogVolume, known := catalogVolume(in.catalog, in.identity.Volume.VolumeID)
	if !known {
		if in.namespaceAbsent || len(in.marked) != 0 {
			return PlanResult{}, &Refusal{
				Code: "adoption_deferred",
				Message: fmt.Sprintf(
					"cartridge carries volume %s, not in the catalog, with %d committed snapshots; adoption/reformat of non-blank tapes is deferred",
					in.identity.Volume.VolumeID,
					len(in.marked),
				),
			}
		}
		return s.initPlan(in, persistInit)
	}
	if catalogVolume.TapeID != in.identity.Volume.TapeID ||
		catalogVolume.TapeID != in.identity.Cartridge.TapeID {
		return PlanResult{}, &Refusal{
			Code: "divergence_deferred",
			Message: fmt.Sprintf(
				"loaded volume %s tape identity %s does not match catalog tape %s",
				catalogVolume.VolumeID,
				in.identity.Volume.TapeID,
				catalogVolume.TapeID,
			),
		}
	}
	if in.namespaceAbsent && len(catalogVolume.Snapshots) == 0 {
		return PlanResult{}, &Refusal{
			Code:    "divergence_deferred",
			Message: fmt.Sprintf("recognized volume %s snapshots namespace is absent", catalogVolume.VolumeID),
		}
	}
	debris, mismatch := classifyInventory(in.library, catalogVolume, in.marked)
	if mismatch != "" {
		return PlanResult{}, &Refusal{Code: "divergence_deferred", Message: mismatch}
	}
	report, err := planner.ConsultTarget(
		in.library,
		in.catalog,
		planner.Target{
			Kind:          planner.TargetKnown,
			VolumeID:      catalogVolume.VolumeID,
			TapeID:        catalogVolume.TapeID,
			CapacityBytes: in.total,
			FreeBytes:     in.free,
		},
		s.deps.FreeSpaceMargin,
	)
	if err != nil {
		return PlanResult{}, err
	}
	assignment, ok := findAssignment(report, planner.TargetKnown)
	if !ok {
		return PlanResult{
			Classification: "fill",
			Debris:         debris,
		}, nil
	}
	plan, err := s.materializeFill(in.catalog, assignment)
	if err != nil {
		return PlanResult{}, err
	}
	return PlanResult{
		Classification: "fill",
		Plan:           plan,
		Persisted:      false,
		Debris:         debris,
	}, nil
}

func (s *Service) initPlan(in inspection, persist bool) (PlanResult, error) {
	target := planner.Target{
		Kind:          planner.TargetBlank,
		TapeID:        in.identity.Cartridge.TapeID,
		CapacityBytes: in.total,
		FreeBytes:     in.free,
	}
	report, err := planner.ConsultTarget(
		in.library,
		in.catalog,
		target,
		s.deps.FreeSpaceMargin,
	)
	if err != nil {
		return PlanResult{}, err
	}
	assignment, ok := findAssignment(report, planner.TargetBlank)
	if !ok {
		assignment = planner.Assignment{
			Target: target,
			Series: []planner.Series{},
		}
	}
	plan, err := s.materializeInit(assignment, in.identity.MediumSerial)
	if err != nil {
		return PlanResult{}, err
	}
	if persist {
		if err := backupplan.Draft(s.deps.StateRoot, plan); err != nil {
			return PlanResult{}, err
		}
	}
	return PlanResult{
		Classification: "init",
		Plan:           plan,
		Persisted:      persist,
		Debris:         []string{},
	}, nil
}

func (s *Service) selectRunPlanLocked(in inspection) (backupplan.Plan, error) {
	ready, err := backupplan.ListReady(s.deps.StateRoot)
	if err != nil {
		return backupplan.Plan{}, err
	}
	for _, plan := range ready {
		if !matchesInspection(plan, in.identity) {
			continue
		}
		if !isInitPlan(plan) {
			if err := validatePlanInspection(plan, in); err != nil {
				return backupplan.Plan{}, err
			}
		}
		return plan, nil
	}
	return s.selectDraftOrFreshLocked(in)
}

func (s *Service) selectDraftOrFreshLocked(in inspection) (backupplan.Plan, error) {
	drafts, err := backupplan.ListDraft(s.deps.StateRoot)
	if err != nil {
		return backupplan.Plan{}, err
	}
	for _, plan := range drafts {
		if !matchesInspection(plan, in.identity) {
			continue
		}
		if isInitPlan(plan) {
			return backupplan.Plan{}, ErrApprovalRequired
		}
		if err := validatePlanInspection(plan, in); err == nil {
			return plan, nil
		}
		if err := backupplan.Discard(s.deps.StateRoot, plan.PlanID); err != nil {
			return backupplan.Plan{}, err
		}
		break
	}
	result, err := s.planFromInspection(in, false)
	if err != nil {
		return backupplan.Plan{}, err
	}
	if result.Classification == "init" {
		if err := backupplan.Draft(s.deps.StateRoot, result.Plan); err != nil {
			return backupplan.Plan{}, err
		}
		return backupplan.Plan{}, ErrApprovalRequired
	}
	if result.Plan.PlanID == "" {
		return backupplan.Plan{}, ErrNoWork
	}
	if err := backupplan.Draft(s.deps.StateRoot, result.Plan); err != nil {
		return backupplan.Plan{}, err
	}
	return result.Plan, nil
}

func validatePlanInspection(plan backupplan.Plan, in inspection) error {
	if plan.Target.TapeID != in.identity.Cartridge.TapeID ||
		in.identity.State != executor.LoadedIdentityIdentified {
		return &Refusal{Code: "prestate_mismatch", Message: "loaded cartridge identity does not match draft"}
	}
	catalogVolume, ok := catalogVolume(in.catalog, in.identity.Volume.VolumeID)
	if !ok {
		return &Refusal{Code: "adoption_deferred", Message: "draft target is not in the catalog"}
	}
	debris, mismatch := classifyInventory(in.library, catalogVolume, in.marked)
	if mismatch != "" {
		return &Refusal{Code: "divergence_deferred", Message: mismatch}
	}
	expected := leadingSnapshots(plan)
	actual := snapshotNames(catalogVolume.Snapshots)
	if !slices.Equal(expected, actual) {
		return &Refusal{Code: "prestate_mismatch", Message: "draft catalog belief no longer matches"}
	}
	_ = debris
	return nil
}

func (s *Service) materializeFill(
	catalog planner.CatalogSnapshot,
	assignment planner.Assignment,
) (backupplan.Plan, error) {
	catalogVolume, ok := catalogVolume(catalog, assignment.Target.VolumeID)
	if !ok {
		return backupplan.Plan{}, fmt.Errorf(
			"tape service: catalog target volume %s does not exist",
			assignment.Target.VolumeID,
		)
	}
	leading := snapshotNames(catalogVolume.Snapshots)
	actions := []backupplan.Action{
		{Type: backupplan.ActionAssertVolume, VolumeID: catalogVolume.VolumeID},
		{Type: backupplan.ActionAssertInventory, Snapshots: leading},
	}
	actions = append(actions, backupActions(assignment.Series)...)
	trailing := append(slices.Clone(leading), namesForSeries(assignment.Series)...)
	slices.Sort(trailing)
	actions = append(actions, backupplan.Action{
		Type:      backupplan.ActionAssertInventory,
		Snapshots: trailing,
	})
	return s.newPlan(catalogVolume.TapeID, "", actions), nil
}

func (s *Service) materializeInit(
	assignment planner.Assignment,
	mediumSerial string,
) (backupplan.Plan, error) {
	volumeID, err := volume.ParseID(s.deps.NewID())
	if err != nil {
		return backupplan.Plan{}, fmt.Errorf("tape service: mint volume ID: %w", err)
	}
	actions := []backupplan.Action{
		{Type: backupplan.ActionReformat},
		{Type: backupplan.ActionAdmit, VolumeID: volumeID},
	}
	actions = append(actions, backupActions(assignment.Series)...)
	actions = append(actions, backupplan.Action{
		Type:      backupplan.ActionAssertInventory,
		Snapshots: namesForSeries(assignment.Series),
	})
	return s.newPlan(assignment.Target.TapeID, mediumSerial, actions), nil
}

func (s *Service) newPlan(
	tapeID tape.ID,
	mediumSerial string,
	actions []backupplan.Action,
) backupplan.Plan {
	return backupplan.Plan{
		PlanID:    s.deps.NewID(),
		CreatedAt: s.now(),
		CreatedBy: s.deps.Creator,
		Target: backupplan.Target{
			TapeID:       tapeID,
			MediumSerial: mediumSerial,
		},
		Actions: actions,
	}
}

func (s *Service) snapshots() (planner.LibrarySnapshot, planner.CatalogSnapshot, error) {
	library, err := planner.ReadLibrarySnapshot(s.deps.LibraryRoot, s.deps.FreeSpaceMargin)
	if err != nil {
		return planner.LibrarySnapshot{}, planner.CatalogSnapshot{}, err
	}
	catalog, err := planner.ReadCatalogSnapshot(s.deps.StateRoot)
	if err != nil {
		return planner.LibrarySnapshot{}, planner.CatalogSnapshot{}, err
	}
	return library, catalog, nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC().Truncate(time.Second)
}

func (s *Service) discardTargetedSuperseded(tapeID tape.ID) error {
	drafts, err := backupplan.ListDraft(s.deps.StateRoot)
	if err != nil {
		return err
	}
	for _, plan := range drafts {
		if plan.Target.TapeID == tapeID && !isInitPlan(plan) {
			return backupplan.Discard(s.deps.StateRoot, plan.PlanID)
		}
	}
	ready, err := backupplan.ListReady(s.deps.StateRoot)
	if err != nil {
		return err
	}
	for _, plan := range ready {
		if plan.Target.TapeID == tapeID {
			return backupplan.Discard(s.deps.StateRoot, plan.PlanID)
		}
	}
	return nil
}

func (s *Service) driveStatus(inSession bool) (DriveStatus, error) {
	if inSession {
		return DriveStatus{State: "in_session"}, nil
	}
	availability, err := executor.ClassifyDriveAvailability(
		s.deps.Drive,
		executor.DriveAvailabilityOptions{LTFSRoot: s.deps.LTFSRoot},
	)
	if err != nil {
		return DriveStatus{}, err
	}
	if availability.CloseRequired {
		if err := s.deps.Drive.Close(); err != nil {
			return DriveStatus{}, fmt.Errorf("tape service: close status probe: %w", err)
		}
	}
	state := string(availability.Reason)
	if availability.Reason == executor.DriveReady ||
		availability.Reason == executor.DriveWaitingForTape {
		state = "idle"
	}
	return DriveStatus{State: state, Message: availability.Message}, nil
}

func markedSnapshots(root string) (
	names []string,
	namespaceAbsent bool,
	err error,
) {
	dir := tapevolume.SnapshotsDir(root)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("tape service: enumerate snapshots namespace: %w", err)
	}
	names = make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, _, err := snapshotname.Parse(entry.Name()); err != nil {
			continue
		}
		marker, err := os.Lstat(filepath.Join(dir, entry.Name(), "complete.json"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, false, fmt.Errorf("tape service: inspect snapshot %q: %w", entry.Name(), err)
		}
		if marker.Mode().IsRegular() && marker.Mode()&os.ModeSymlink == 0 {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names, false, nil
}

func classifyInventory(
	library planner.LibrarySnapshot,
	catalog planner.Volume,
	marked []string,
) (debris []string, mismatch string) {
	expected := snapshotNames(catalog.Snapshots)
	missing, extra := difference(expected, marked)
	indexed := make(map[string]struct{}, len(library.Series))
	for _, series := range library.Series {
		indexed[series.MetadataRef] = struct{}{}
	}
	debris = make([]string, 0, len(extra))
	foreign := make([]string, 0)
	for _, name := range extra {
		ref, _, _ := snapshotname.Parse(name)
		if _, ok := indexed[ref]; ok {
			debris = append(debris, name)
		} else {
			foreign = append(foreign, name)
		}
	}
	if len(missing) != 0 || len(foreign) != 0 {
		return debris, fmt.Sprintf(
			"tape's contents diverge from the catalog (missing=%v extra=%v); divergence handling is deferred",
			missing,
			foreign,
		)
	}
	return debris, ""
}

func difference(expected, actual []string) (missing, extra []string) {
	want := make(map[string]struct{}, len(expected))
	got := make(map[string]struct{}, len(actual))
	for _, name := range expected {
		want[name] = struct{}{}
	}
	for _, name := range actual {
		got[name] = struct{}{}
	}
	missing = make([]string, 0)
	extra = make([]string, 0)
	for name := range want {
		if _, ok := got[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			extra = append(extra, name)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	return missing, extra
}

func findAssignment(
	report planner.Report,
	kind planner.TargetKind,
) (planner.Assignment, bool) {
	for _, assignment := range report.Assignments {
		if assignment.Target.Kind == kind {
			return assignment, true
		}
	}
	return planner.Assignment{}, false
}

func catalogVolume(catalog planner.CatalogSnapshot, id volume.ID) (planner.Volume, bool) {
	for _, candidate := range catalog.Volumes {
		if candidate.VolumeID == id {
			return candidate, true
		}
	}
	return planner.Volume{}, false
}

func snapshotNames(snapshots []planner.Snapshot) []string {
	names := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		name, err := snapshotname.Format(snapshot.MetadataRef, snapshot.Generation)
		if err == nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func namesForSeries(series []planner.Series) []string {
	names := make([]string, 0, len(series))
	for _, item := range series {
		name, _ := snapshotname.Format(item.MetadataRef, item.Generation)
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func backupActions(series []planner.Series) []backupplan.Action {
	actions := make([]backupplan.Action, 0, len(series))
	for _, item := range series {
		actions = append(actions, backupplan.Action{
			Type:               backupplan.ActionBackup,
			MetadataRef:        item.MetadataRef,
			RootPath:           item.RootPath,
			Generation:         item.Generation,
			PayloadFingerprint: item.PayloadFingerprint,
			Bytes:              item.Bytes,
		})
	}
	return actions
}

func leadingSnapshots(plan backupplan.Plan) []string {
	for _, action := range plan.Actions {
		if action.Type == backupplan.ActionBackup {
			break
		}
		if action.Type == backupplan.ActionAssertInventory {
			result := slices.Clone(action.Snapshots)
			slices.Sort(result)
			return result
		}
	}
	return []string{}
}

func isInitPlan(plan backupplan.Plan) bool {
	for _, action := range plan.Actions {
		if action.Type == backupplan.ActionReformat || action.Type == backupplan.ActionAdmit {
			return true
		}
	}
	return false
}

func matchesInspection(plan backupplan.Plan, identity executor.LoadedIdentity) bool {
	if identity.State == executor.LoadedIdentityUnidentified {
		return isInitPlan(plan) && plan.Target.MediumSerial == identity.MediumSerial
	}
	return identity.State == executor.LoadedIdentityIdentified &&
		plan.Target.TapeID == identity.Cartridge.TapeID
}

func withoutClaims(
	library planner.LibrarySnapshot,
	plans []backupplan.Plan,
) planner.LibrarySnapshot {
	claimed := make(map[planner.Pair]struct{})
	for _, plan := range plans {
		for _, action := range plan.Actions {
			if action.Type == backupplan.ActionBackup {
				claimed[planner.Pair{
					MetadataRef: action.MetadataRef,
					Generation:  action.Generation,
				}] = struct{}{}
			}
		}
	}
	filtered := library.Series[:0]
	for _, series := range library.Series {
		if _, ok := claimed[planner.Pair{
			MetadataRef: series.MetadataRef,
			Generation:  series.Generation,
		}]; !ok {
			filtered = append(filtered, series)
		}
	}
	library.Series = filtered
	return library
}

func withoutTapes(
	catalog planner.CatalogSnapshot,
	plans []backupplan.Plan,
) planner.CatalogSnapshot {
	excluded := make(map[tape.ID]struct{}, len(plans))
	for _, plan := range plans {
		excluded[plan.Target.TapeID] = struct{}{}
	}
	filtered := catalog.Volumes[:0]
	for _, catalogVolume := range catalog.Volumes {
		if _, ok := excluded[catalogVolume.TapeID]; !ok {
			filtered = append(filtered, catalogVolume)
		}
	}
	catalog.Volumes = filtered
	return catalog
}

func initDraftStatuses(
	plans []backupplan.Plan,
	now time.Time,
) []planner.InitPlanAwaitingApproval {
	statuses := make([]planner.InitPlanAwaitingApproval, 0)
	for _, plan := range plans {
		if !isInitPlan(plan) {
			continue
		}
		claimed := make([]planner.Pair, 0)
		for _, action := range plan.Actions {
			if action.Type == backupplan.ActionBackup {
				claimed = append(claimed, planner.Pair{
					MetadataRef: action.MetadataRef,
					Generation:  action.Generation,
				})
			}
		}
		statuses = append(statuses, planner.InitPlanAwaitingApproval{
			PlanID:     plan.PlanID,
			TapeID:     plan.Target.TapeID,
			CreatedAt:  plan.CreatedAt,
			AgeSeconds: max(0, int64(now.Sub(plan.CreatedAt)/time.Second)),
			Claimed:    claimed,
		})
	}
	return statuses
}

func cloneLiveSessions(live map[string]LiveSession) []LiveSession {
	result := make([]LiveSession, 0, len(live))
	for _, session := range live {
		session.Plans = slices.Clone(session.Plans)
		result = append(result, session)
	}
	slices.SortFunc(result, func(a, b LiveSession) int {
		return strings.Compare(a.SessionID, b.SessionID)
	})
	return result
}

func refuseStubOperation(operation string) error {
	switch operation {
	case "", "backup":
		return nil
	case "compaction":
		return &Refusal{Code: "compaction_deferred", Message: "compaction is deferred"}
	case "sync", "import":
		return &Refusal{Code: "sync_deferred", Message: "sync-to-catalog is deferred"}
	case "recovery", "restore":
		return &Refusal{Code: "recovery_deferred", Message: "recovery is deferred"}
	default:
		return fmt.Errorf("tape service: unsupported operation %q", operation)
	}
}

func reportPath(stateRoot string) string {
	return filepath.Join(stateRoot, "report.json")
}

func writeReport(stateRoot string, report ConsultResult) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("tape service: encode consult report: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(stateRoot, 0o775); err != nil {
		return fmt.Errorf("tape service: create state root: %w", err)
	}
	temp, err := os.CreateTemp(stateRoot, ".report-")
	if err != nil {
		return fmt.Errorf("tape service: create report temporary: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("tape service: write report: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("tape service: sync report: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("tape service: close report: %w", err)
	}
	if err := os.Rename(tempPath, reportPath(stateRoot)); err != nil {
		return fmt.Errorf("tape service: install report: %w", err)
	}
	return nil
}

func readReport(stateRoot string) (*ConsultResult, error) {
	data, err := os.ReadFile(reportPath(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tape service: read consult report: %w", err)
	}
	var report ConsultResult
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("tape service: decode consult report: %w", err)
	}
	if report.SchemaVersion != reportSchemaVersion {
		return nil, fmt.Errorf(
			"tape service: unsupported report schemaVersion %d",
			report.SchemaVersion,
		)
	}
	return &report, nil
}
