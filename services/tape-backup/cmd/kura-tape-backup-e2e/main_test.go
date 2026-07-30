//go:build e2e_stub

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

func TestStubBlankWalkthroughAndFillCeremony(t *testing.T) {
	state := &stubState{}
	handler := state.handler("e2e-token")

	consult := request(t, handler, http.MethodPost, "/api/tape/consult", `[{"tapeID":"BLK001L6"},{"tapeID":"BLK002L6"}]`)
	if consult.Code != http.StatusOK {
		t.Fatalf("consult status = %d, want %d", consult.Code, http.StatusOK)
	}
	var consultResult tapeapi.ConsultResult
	decodeRecorder(t, consult, &consultResult)
	if consultResult.Report.Sizing.BringBlanks != 2 {
		t.Fatalf("consult bringBlanks = %d, want 2", consultResult.Report.Sizing.BringBlanks)
	}

	plan := request(t, handler, http.MethodPost, "/api/tape/plan", `{"tapeID":"BLK001L6"}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d, want %d", plan.Code, http.StatusOK)
	}
	var planResult tapeapi.PlanResult
	decodeRecorder(t, plan, &planResult)
	if planResult.Classification != "init" ||
		planResult.Plan.PlanID != initPlanID ||
		planResult.Plan.Target.MediumSerial != "MAM-SERIAL-BLANK-001" ||
		!planResult.Persisted {
		t.Fatalf("plan result = %+v, want exact persisted init fixture", planResult)
	}

	approve := request(
		t,
		handler,
		http.MethodPost,
		"/api/tape/approve/"+initPlanID,
		"",
	)
	if approve.Code != http.StatusNoContent {
		t.Fatalf("approve status = %d, want %d", approve.Code, http.StatusNoContent)
	}

	run := request(t, handler, http.MethodPost, "/api/tape/run", `{"tapeID":"BLK001L6"}`)
	if run.Code != http.StatusOK {
		t.Fatalf("run status = %d, want %d", run.Code, http.StatusOK)
	}
	var runResult tapeapi.RunResult
	decodeRecorder(t, run, &runResult)
	if runResult.Classification != "init" || runResult.Plan.PlanID != initPlanID {
		t.Fatalf("run result = %+v, want exact init plan", runResult)
	}

	next := request(t, handler, http.MethodPost, "/api/tape/consult", `[]`)
	var nextResult tapeapi.ConsultResult
	decodeRecorder(t, next, &nextResult)
	if nextResult.Report.Sizing.PendingBytes != 0 ||
		nextResult.Report.Sizing.BringBlanks != 0 {
		t.Fatalf("next sizing = %+v, want no pending work", nextResult.Report.Sizing)
	}

	fill := request(t, handler, http.MethodPost, "/api/tape/plan", `{"tapeID":"FILL01L6"}`)
	var fillResult tapeapi.PlanResult
	decodeRecorder(t, fill, &fillResult)
	if fillResult.Classification != "fill" || fillResult.Persisted {
		t.Fatalf("fill result = %+v, want ephemeral fill", fillResult)
	}
	fillRun := request(t, handler, http.MethodPost, "/api/tape/run", `{"tapeID":"FILL01L6"}`)
	if fillRun.Code != http.StatusOK {
		t.Fatalf("fill run status = %d, want %d", fillRun.Code, http.StatusOK)
	}
}

func TestStubHeaderedInitAttestationFacts(t *testing.T) {
	handler := (&stubState{}).handler("e2e-token")
	response := request(
		t,
		handler,
		http.MethodPost,
		"/api/tape/plan",
		`{"tapeID":"HDR001L6"}`,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("plan status = %d, want %d", response.Code, http.StatusOK)
	}
	var result tapeapi.PlanResult
	decodeRecorder(t, response, &result)
	want := tapeapi.PlanTarget{
		TapeID:        "HDR001L6",
		MediumSerial:  "MAM-SERIAL-HEADERED-001",
		VolumeID:      "01BX5ZZKBKACTAV9WEVGEMMVS2",
		UsedBytes:     12 * gib,
		FreeBytes:     88 * gib,
		CapacityBytes: 100 * gib,
	}
	if result.Classification != "init" || result.Target != want {
		t.Fatalf("plan result = %+v, want init target %+v", result, want)
	}
}

func TestStubRefusalCodesExact(t *testing.T) {
	handler := (&stubState{}).handler("e2e-token")
	tests := []struct {
		tapeID string
		code   string
	}{
		{tapeID: "DIV001L6", code: "divergence_deferred"},
		{tapeID: "ADP001L6", code: "adoption_deferred"},
		{tapeID: "IDN001L6", code: "identity_unconfirmed"},
		{tapeID: "CMP001L6", code: "compaction_deferred"},
		{tapeID: "SYN001L6", code: "sync_deferred"},
		{tapeID: "REC001L6", code: "recovery_deferred"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			response := request(
				t,
				handler,
				http.MethodPost,
				"/api/tape/plan",
				`{"tapeID":"`+test.tapeID+`"}`,
			)
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
			}
			var envelope struct {
				Kind     string `json:"kind"`
				Category string `json:"category"`
			}
			decodeRecorder(t, response, &envelope)
			if envelope.Kind != test.code || envelope.Category != "invalid_params" {
				t.Fatalf("envelope = %+v, want code %q and invalid_params", envelope, test.code)
			}
		})
	}
}

func TestStubRunReturnsConsultedBeliefDraftVerbatim(t *testing.T) {
	handler := (&stubState{}).handler("e2e-token")
	consult := request(t, handler, http.MethodPost, "/api/tape/consult", `[]`)
	var consultResult tapeapi.ConsultResult
	decodeRecorder(t, consult, &consultResult)
	run := request(t, handler, http.MethodPost, "/api/tape/run", `{"tapeID":"ABC123L6"}`)
	var runResult tapeapi.RunResult
	decodeRecorder(t, run, &runResult)
	if len(consultResult.Drafts) != 1 ||
		runResult.Plan.PlanID != consultResult.Drafts[0].PlanID {
		t.Fatalf(
			"run plan = %q, consulted drafts = %+v",
			runResult.Plan.PlanID,
			consultResult.Drafts,
		)
	}
	gotSeries := backupRefs(runResult.Plan)
	wantSeries := []string{"tvdb:alpha", "tvdb:beta"}
	if len(gotSeries) != len(wantSeries) ||
		gotSeries[0] != wantSeries[0] ||
		gotSeries[1] != wantSeries[1] {
		t.Fatalf("run series = %v, want %v", gotSeries, wantSeries)
	}
}

func request(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer e2e-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func decodeRecorder(t *testing.T, response *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, response.Body.String())
	}
}

func backupRefs(plan tapeapi.Plan) []string {
	refs := make([]string, 0)
	for _, action := range plan.Actions {
		if action.Type == "backup" && action.MetadataRef != nil {
			refs = append(refs, *action.MetadataRef)
		}
	}
	return refs
}
