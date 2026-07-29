package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/server/auth"
	"github.com/wyvernzora/kura/services/tape-backup/internal/service"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

type fakeTapeService struct {
	statusFunc  func() (service.StatusResult, error)
	consultFunc func([]planner.Blank) (service.ConsultResult, error)
	planFunc    func(service.PlanRequest) (service.PlanResult, error)
	runFunc     func(context.Context, service.PlanRequest) (service.RunResult, error)
	approveFunc func(string) error
	discardFunc func(string) error
}

func (f *fakeTapeService) Status() (service.StatusResult, error) {
	if f.statusFunc == nil {
		return service.StatusResult{}, nil
	}
	return f.statusFunc()
}

func (f *fakeTapeService) Consult(blanks []planner.Blank) (service.ConsultResult, error) {
	if f.consultFunc == nil {
		return service.ConsultResult{}, nil
	}
	return f.consultFunc(blanks)
}

func (f *fakeTapeService) Plan(request service.PlanRequest) (service.PlanResult, error) {
	if f.planFunc == nil {
		return service.PlanResult{}, nil
	}
	return f.planFunc(request)
}

func (f *fakeTapeService) Run(
	ctx context.Context,
	request service.PlanRequest,
) (service.RunResult, error) {
	if f.runFunc == nil {
		return service.RunResult{}, nil
	}
	return f.runFunc(ctx, request)
}

func (f *fakeTapeService) Approve(planID string) error {
	if f.approveFunc == nil {
		return nil
	}
	return f.approveFunc(planID)
}

func (f *fakeTapeService) Discard(planID string) error {
	if f.discardFunc == nil {
		return nil
	}
	return f.discardFunc(planID)
}

func TestHandlersMapSixVerbs(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		want := service.StatusResult{
			LiveSessions: []service.LiveSession{},
			Drive:        service.DriveStatus{State: "idle"},
			InitDrafts:   []planner.InitPlanAwaitingApproval{},
		}
		fake := &fakeTapeService{
			statusFunc: func() (service.StatusResult, error) {
				return want, nil
			},
		}
		response := serveRequest(t, fake, http.MethodGet, "/api/tape/status", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		var got service.StatusResult
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("response = %+v, want %+v", got, want)
		}
	})

	t.Run("consult", func(t *testing.T) {
		wantBlanks := []planner.Blank{{TapeID: "ABC123L6"}}
		wantResult := service.ConsultResult{
			SchemaVersion: 1,
			CreatedAt:     time.Date(2026, time.July, 29, 1, 2, 3, 0, time.UTC),
			Drafts:        []backupplan.Plan{},
		}
		fake := &fakeTapeService{
			consultFunc: func(blanks []planner.Blank) (service.ConsultResult, error) {
				if !reflect.DeepEqual(blanks, wantBlanks) {
					t.Fatalf("Consult() blanks = %+v, want %+v", blanks, wantBlanks)
				}
				return wantResult, nil
			},
		}
		response := serveRequest(
			t,
			fake,
			http.MethodPost,
			"/api/tape/consult",
			[]byte(`[{"tapeID":"ABC123L6"}]`),
		)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		var got service.ConsultResult
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !reflect.DeepEqual(got, wantResult) {
			t.Fatalf("response = %+v, want %+v", got, wantResult)
		}
	})

	t.Run("plan", func(t *testing.T) {
		wantRequest := service.PlanRequest{TapeID: "ABC123L6", Operation: "backup"}
		fake := &fakeTapeService{
			planFunc: func(request service.PlanRequest) (service.PlanResult, error) {
				if request != wantRequest {
					t.Fatalf("Plan() request = %+v, want %+v", request, wantRequest)
				}
				return service.PlanResult{
					Classification: "fill",
					Plan: backupplan.Plan{
						PlanID:  "01KTESTPLAN000000000000000",
						Target:  backupplan.Target{TapeID: "ABC123L6"},
						Actions: []backupplan.Action{},
					},
					Debris: []string{},
				}, nil
			},
		}
		response := serveRequest(
			t,
			fake,
			http.MethodPost,
			"/api/tape/plan",
			[]byte(`{"tapeID":"ABC123L6","operation":"backup"}`),
		)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		var plan map[string]json.RawMessage
		if err := json.Unmarshal(got["plan"], &plan); err != nil {
			t.Fatalf("decode plan: %v", err)
		}
		if string(plan["schemaVersion"]) != "1" {
			t.Fatalf("plan schemaVersion = %s, want 1", plan["schemaVersion"])
		}
	})

	t.Run("run", func(t *testing.T) {
		wantRequest := service.PlanRequest{TapeID: "ABC123L6"}
		wantResult := service.RunResult{
			Classification: "fill",
			Plan: backupplan.Plan{
				PlanID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
				Target: backupplan.Target{TapeID: "ABC123L6"},
			},
		}
		fake := &fakeTapeService{
			runFunc: func(
				ctx context.Context,
				request service.PlanRequest,
			) (service.RunResult, error) {
				if ctx == nil {
					t.Fatal("Run() context is nil")
				}
				if request != wantRequest {
					t.Fatalf("Run() request = %+v, want %+v", request, wantRequest)
				}
				return wantResult, nil
			},
		}
		response := serveRequest(
			t,
			fake,
			http.MethodPost,
			"/api/tape/run",
			[]byte(`{"tapeID":"ABC123L6"}`),
		)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		var got tapeapi.RunResult
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.Classification != wantResult.Classification ||
			got.Plan.PlanID != wantResult.Plan.PlanID {
			t.Fatalf("response = %+v, want classification and plan ID from %+v", got, wantResult)
		}
	})

	t.Run("approve", func(t *testing.T) {
		fake := &fakeTapeService{
			approveFunc: func(planID string) error {
				if planID != "plan-1" {
					t.Fatalf("Approve() planID = %q, want %q", planID, "plan-1")
				}
				return nil
			},
		}
		response := serveRequest(
			t,
			fake,
			http.MethodPost,
			"/api/tape/approve/plan-1",
			nil,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	})

	t.Run("discard", func(t *testing.T) {
		fake := &fakeTapeService{
			discardFunc: func(planID string) error {
				if planID != "plan-2" {
					t.Fatalf("Discard() planID = %q, want %q", planID, "plan-2")
				}
				return nil
			},
		}
		response := serveRequest(
			t,
			fake,
			http.MethodPost,
			"/api/tape/discard/plan-2",
			nil,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	})
}

func TestRecoveryAndRestoreSurfacesAreStructurallyAbsent(t *testing.T) {
	fake := &fakeTapeService{}
	for _, path := range []string{
		"/api/tape/recovery",
		"/api/tape/restore",
	} {
		t.Run(path, func(t *testing.T) {
			response := serveRequest(t, fake, http.MethodPost, path, []byte(`{}`))
			if response.Code != http.StatusNotFound {
				t.Fatalf(
					"POST %s status = %d, want %d",
					path,
					response.Code,
					http.StatusNotFound,
				)
			}
		})
	}
}

func TestAllSixVerbsRejectMissingBearerToken(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "status", method: http.MethodGet, path: "/api/tape/status"},
		{name: "consult", method: http.MethodPost, path: "/api/tape/consult", body: []byte(`[]`)},
		{name: "plan", method: http.MethodPost, path: "/api/tape/plan", body: []byte(`{}`)},
		{name: "run", method: http.MethodPost, path: "/api/tape/run", body: []byte(`{}`)},
		{name: "approve", method: http.MethodPost, path: "/api/tape/approve/plan-1"},
		{name: "discard", method: http.MethodPost, path: "/api/tape/discard/plan-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, err := NewServer(Deps{
				Service:     &fakeTapeService{},
				BearerToken: "secret",
			})
			if err != nil {
				t.Fatalf("NewServer() error = %v", err)
			}
			request := httptest.NewRequest(test.method, test.path, bytes.NewReader(test.body))
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			const want = "{\"kind\":\"unauthorized\",\"category\":\"invalid_params\",\"message\":\"missing or invalid bearer token\"}\n"
			if response.Body.String() != want {
				t.Fatalf("body = %q, want %q", response.Body.String(), want)
			}
		})
	}
}

func TestRefusalCodesPassThroughExactly(t *testing.T) {
	for _, code := range []string{
		"divergence_deferred",
		"adoption_deferred",
		"identity_unconfirmed",
		"compaction_deferred",
		"sync_deferred",
		"recovery_deferred",
	} {
		t.Run(code, func(t *testing.T) {
			fake := &fakeTapeService{
				planFunc: func(service.PlanRequest) (service.PlanResult, error) {
					return service.PlanResult{}, &service.Refusal{
						Code:    code,
						Message: "refused",
					}
				},
			}
			response := serveRequest(t, fake, http.MethodPost, "/api/tape/plan", []byte(`{}`))
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
			}
			want := `{"kind":"` + code + `","category":"invalid_params","message":"refused"}` + "\n"
			if response.Body.String() != want {
				t.Fatalf("body = %q, want %q", response.Body.String(), want)
			}
		})
	}
}

func TestStatusHandlerIsReadOnly(t *testing.T) {
	statusCalls := 0
	unexpected := errors.New("mutating service method called")
	fake := &fakeTapeService{
		statusFunc: func() (service.StatusResult, error) {
			statusCalls++
			return service.StatusResult{}, nil
		},
		consultFunc: func([]planner.Blank) (service.ConsultResult, error) {
			return service.ConsultResult{}, unexpected
		},
		planFunc: func(service.PlanRequest) (service.PlanResult, error) {
			return service.PlanResult{}, unexpected
		},
		runFunc: func(
			context.Context,
			service.PlanRequest,
		) (service.RunResult, error) {
			return service.RunResult{}, unexpected
		},
		approveFunc: func(string) error { return unexpected },
		discardFunc: func(string) error { return unexpected },
	}

	response := serveRequest(t, fake, http.MethodGet, "/api/tape/status", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if statusCalls != 1 {
		t.Fatalf("Status() calls = %d, want 1", statusCalls)
	}
}

func TestRunHandlerPropagatesRequestCancellation(t *testing.T) {
	fake := &fakeTapeService{
		runFunc: func(
			ctx context.Context,
			_ service.PlanRequest,
		) (service.RunResult, error) {
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatalf("Run() context error = %v, want context.Canceled", ctx.Err())
			}
			return service.RunResult{}, context.Canceled
		},
	}
	server, err := NewServer(Deps{
		Service:     fake,
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/tape/run",
		bytes.NewReader([]byte(`{}`)),
	).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestHTTPServerBasesRequestsOnServeContext(t *testing.T) {
	server, err := NewServer(Deps{Service: &fakeTapeService{}})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	httpServer := newHTTPServer(ctx, server)
	cancel()

	requestContext := httpServer.BaseContext(nil)
	if !errors.Is(requestContext.Err(), context.Canceled) {
		t.Fatalf("request context error = %v, want context.Canceled", requestContext.Err())
	}
}

func TestHealthIsUnauthenticated(t *testing.T) {
	server, err := NewServer(Deps{
		Service:     &fakeTapeService{},
		BearerToken: "secret",
		Version:     "v1.2.3",
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, auth.HealthPath, http.NoBody)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func serveRequest(
	t *testing.T,
	service TapeService,
	method string,
	path string,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	server, err := NewServer(Deps{
		Service:     service,
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
