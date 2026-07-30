//go:build e2e_stub

// Command kura-tape-backup-e2e serves deterministic tape REST fixtures for
// the CLI txtar suite.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

const (
	initPlanID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	fillPlanID = "01BX5ZZKBKACTAV9WEVGEMMVRZ"
	gib        = int64(1024 * 1024 * 1024)
)

var fixedTime = time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)

type stubState struct {
	mu           sync.Mutex
	initDrafted  bool
	initApproved bool
	initRan      bool
	ejected      bool
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	portFile := flag.String("port-file", "", "write the selected port to this file")
	token := flag.String("token", "e2e-token", "required bearer token")
	flag.Parse()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	if *portFile == "" {
		_ = listener.Close()
		return errors.New("--port-file is required")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(*portFile, []byte(fmt.Sprintf("%d\n", port)), 0o600); err != nil {
		_ = listener.Close()
		return err
	}

	state := &stubState{}
	server := &http.Server{
		Handler:           state.handler(*token),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *stubState) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/tape/status", s.status)
	mux.HandleFunc("POST /api/tape/consult", s.consult)
	mux.HandleFunc("POST /api/tape/plan", s.plan)
	mux.HandleFunc("POST /api/tape/run", s.run)
	mux.HandleFunc("POST /api/tape/approve/{planID}", s.approve)
	mux.HandleFunc("POST /api/tape/discard/{planID}", s.discard)
	mux.HandleFunc("POST /api/tape/eject", s.eject)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" &&
			r.Header.Get("Authorization") != "Bearer "+token {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *stubState) status(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := tapeapi.StatusResult{
		LiveSessions: []tapeapi.LiveSession{},
		Drive:        tapeapi.DriveStatus{State: "idle", Message: "ready"},
		InitDrafts:   []tapeapi.InitPlanAwaitingApproval{},
		Consult:      initialConsult(),
	}
	if s.initDrafted && !s.initApproved {
		result.InitDrafts = []tapeapi.InitPlanAwaitingApproval{{
			PlanID:     initPlanID,
			TapeID:     "BLK001L6",
			CreatedAt:  fixedTime,
			AgeSeconds: 30,
			Claimed: []tapeapi.Pair{{
				MetadataRef: "tvdb:blank-a",
				Generation:  1,
			}},
		}}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *stubState) consult(w http.ResponseWriter, r *http.Request) {
	var blanks []tapeapi.Blank
	if err := json.NewDecoder(r.Body).Decode(&blanks); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_ref", "invalid request body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initRan {
		result := tapeapi.ConsultResult{
			SchemaVersion: 1,
			CreatedAt:     fixedTime.Add(time.Minute),
			Report: tapeapi.Report{
				Pending:     []tapeapi.Series{},
				Assignments: []tapeapi.Assignment{},
				Sizing: tapeapi.Sizing{
					NominalBlankMediaGeneration: "LTO-6",
				},
				Shortfall:                 []tapeapi.Series{},
				Deferred:                  []tapeapi.Series{},
				Unbackupable:              []tapeapi.Series{},
				LineageRefusals:           []tapeapi.LineageRefusal{},
				InitPlansAwaitingApproval: []tapeapi.InitPlanAwaitingApproval{},
			},
			Drafts: []tapeapi.Plan{},
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	result := *initialConsult()
	if len(blanks) != 0 {
		result.Report.Sizing.DeclaredBlankRoomBytes = int64(len(blanks)) * 2_500_000_000_000
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *stubState) plan(w http.ResponseWriter, r *http.Request) {
	var request tapeapi.PlanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_ref", "invalid request body")
		return
	}
	if code, ok := refusalForTape(request.TapeID); ok {
		writeError(w, http.StatusConflict, code, refusalMessage(code))
		return
	}
	switch request.TapeID {
	case "BLK001L6":
		s.mu.Lock()
		s.initDrafted = true
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, tapeapi.PlanResult{
			Classification: "init",
			Plan:           initPlan(),
			Target: tapeapi.PlanTarget{
				TapeID:        "BLK001L6",
				MediumSerial:  "MAM-SERIAL-BLANK-001",
				UsedBytes:     20 * gib,
				FreeBytes:     80 * gib,
				CapacityBytes: 100 * gib,
			},
			Persisted: true,
			Debris:    []string{},
		})
	case "HDR001L6":
		writeJSON(w, http.StatusOK, tapeapi.PlanResult{
			Classification: "init",
			Plan:           headeredInitPlan(),
			Target: tapeapi.PlanTarget{
				TapeID:        "HDR001L6",
				MediumSerial:  "MAM-SERIAL-HEADERED-001",
				VolumeID:      "01BX5ZZKBKACTAV9WEVGEMMVS2",
				UsedBytes:     12 * gib,
				FreeBytes:     88 * gib,
				CapacityBytes: 100 * gib,
			},
			Persisted: true,
			Debris:    []string{},
		})
	case "FILL01L6", "ABC123L6":
		writeJSON(w, http.StatusOK, tapeapi.PlanResult{
			Classification: "fill",
			Plan:           fillPlan(request.TapeID),
			Persisted:      false,
			Debris:         []string{},
		})
	default:
		writeError(
			w,
			http.StatusConflict,
			"identity_unconfirmed",
			"unidentified media requires the declared cartridge label",
		)
	}
}

func (s *stubState) approve(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("planID") != initPlanID {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initDrafted {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	s.initApproved = true
	w.WriteHeader(http.StatusNoContent)
}

func (s *stubState) run(w http.ResponseWriter, r *http.Request) {
	var request tapeapi.PlanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_ref", "invalid request body")
		return
	}
	if request.TapeID == "BLK001L6" {
		s.mu.Lock()
		if !s.initApproved {
			s.mu.Unlock()
			writeError(
				w,
				http.StatusConflict,
				"approval_required",
				"tape service: init plan is awaiting approval",
			)
			return
		}
		s.initRan = true
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, tapeapi.RunResult{
			Classification: "init",
			Plan:           initPlan(),
		})
		return
	}
	if request.TapeID == "ABC123L6" || request.TapeID == "FILL01L6" {
		writeJSON(w, http.StatusOK, tapeapi.RunResult{
			Classification: "fill",
			Plan:           fillPlan(request.TapeID),
		})
		return
	}
	writeError(w, http.StatusConflict, "identity_unconfirmed", refusalMessage("identity_unconfirmed"))
}

func (s *stubState) discard(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.initDrafted = false
	s.initApproved = false
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stubState) eject(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.ejected = true
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func initialConsult() *tapeapi.ConsultResult {
	assignmentSeries := []tapeapi.Series{
		{
			MetadataRef:        "tvdb:alpha",
			RootPath:           "Alpha",
			Generation:         1,
			PayloadFingerprint: strings.Repeat("a", 64),
			Bytes:              60 * gib,
			Eligibility:        "eligible",
		},
		{
			MetadataRef:        "tvdb:beta",
			RootPath:           "Beta",
			Generation:         2,
			PayloadFingerprint: strings.Repeat("b", 64),
			Bytes:              40 * gib,
			Eligibility:        "eligible",
		},
	}
	return &tapeapi.ConsultResult{
		SchemaVersion: 1,
		CreatedAt:     fixedTime,
		Report: tapeapi.Report{
			Pending: assignmentSeries,
			Assignments: []tapeapi.Assignment{{
				Target: tapeapi.Target{
					Kind:          "known",
					VolumeID:      "01BX5ZZKBKACTAV9WEVGEMMVS0",
					TapeID:        "ABC123L6",
					CapacityBytes: 300 * gib,
					FreeBytes:     100 * gib,
				},
				Series: assignmentSeries,
				Bytes:  100 * gib,
			}},
			Sizing: tapeapi.Sizing{
				PendingBytes:                300 * gib,
				KnownRoomBytes:              100 * gib,
				RosterBytes:                 100 * gib,
				ShortfallBytes:              200 * gib,
				NominalBlankCapacityBytes:   100 * gib,
				NominalBlankUsableBytes:     100 * gib,
				NominalBlankMediaGeneration: "LTO-6",
				BringBlanks:                 2,
			},
			Shortfall:                 []tapeapi.Series{},
			Deferred:                  []tapeapi.Series{},
			Unbackupable:              []tapeapi.Series{},
			LineageRefusals:           []tapeapi.LineageRefusal{},
			InitPlansAwaitingApproval: []tapeapi.InitPlanAwaitingApproval{},
		},
		Drafts: []tapeapi.Plan{fillPlan("ABC123L6")},
	}
}

func initPlan() tapeapi.Plan {
	serial := "MAM-SERIAL-BLANK-001"
	volumeID := "01BX5ZZKBKACTAV9WEVGEMMVS1"
	metadataRef := "tvdb:blank-a"
	rootPath := "Blank A"
	generation := 1
	fingerprint := strings.Repeat("c", 64)
	bytes := int64(20 * 1024 * 1024 * 1024)
	snapshots := []string{"OZUWIZJAOM5DCYLOM5QQ.g1"}
	return tapeapi.Plan{
		SchemaVersion: 1,
		PlanID:        initPlanID,
		CreatedAt:     fixedTime,
		CreatedBy:     tapeapi.Creator{Version: "e2e", Host: "stub"},
		Target: tapeapi.PlanTarget{
			TapeID:       "BLK001L6",
			MediumSerial: serial,
		},
		Actions: []tapeapi.Action{
			{Type: "reformat"},
			{Type: "admit", VolumeID: &volumeID},
			{
				Type:               "backup",
				MetadataRef:        &metadataRef,
				RootPath:           &rootPath,
				Generation:         &generation,
				PayloadFingerprint: &fingerprint,
				Bytes:              &bytes,
			},
			{Type: "assert_inventory", Snapshots: &snapshots},
		},
	}
}

func headeredInitPlan() tapeapi.Plan {
	plan := initPlan()
	plan.Target.TapeID = "HDR001L6"
	plan.Target.MediumSerial = "MAM-SERIAL-HEADERED-001"
	return plan
}

func fillPlan(tapeID string) tapeapi.Plan {
	volumeID := "01BX5ZZKBKACTAV9WEVGEMMVS0"
	empty := []string{}
	final := []string{"OR2WY5B2MFWHI.g1", "OR2WY5B2MJSXIYI.g2"}
	alpha := "tvdb:alpha"
	beta := "tvdb:beta"
	alphaGeneration := 1
	betaGeneration := 2
	return tapeapi.Plan{
		SchemaVersion: 1,
		PlanID:        fillPlanID,
		CreatedAt:     fixedTime,
		CreatedBy:     tapeapi.Creator{Version: "e2e", Host: "stub"},
		Target:        tapeapi.PlanTarget{TapeID: tapeID},
		Actions: []tapeapi.Action{
			{Type: "assert_volume", VolumeID: &volumeID},
			{Type: "assert_inventory", Snapshots: &empty},
			{Type: "backup", MetadataRef: &alpha, Generation: &alphaGeneration},
			{Type: "backup", MetadataRef: &beta, Generation: &betaGeneration},
			{Type: "assert_inventory", Snapshots: &final},
		},
	}
}

func refusalForTape(tapeID string) (string, bool) {
	codes := map[string]string{
		"DIV001L6": "divergence_deferred",
		"ADP001L6": "adoption_deferred",
		"IDN001L6": "identity_unconfirmed",
		"CMP001L6": "compaction_deferred",
		"SYN001L6": "sync_deferred",
		"REC001L6": "recovery_deferred",
	}
	code, ok := codes[tapeID]
	return code, ok
}

func refusalMessage(code string) string {
	messages := map[string]string{
		"divergence_deferred":  "tape's contents diverge from the catalog; divergence handling is deferred",
		"adoption_deferred":    "adoption/reformat of non-blank tapes is deferred",
		"identity_unconfirmed": "unidentified media requires an approved init plan",
		"compaction_deferred":  "compaction is deferred",
		"sync_deferred":        "sync-to-catalog is deferred",
		"recovery_deferred":    "recovery is deferred",
	}
	return messages[code]
}

func writeError(w http.ResponseWriter, status int, kind, message string) {
	writeJSON(w, status, struct {
		Kind     string `json:"kind"`
		Category string `json:"category"`
		Message  string `json:"message"`
	}{
		Kind:     kind,
		Category: "invalid_params",
		Message:  message,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
