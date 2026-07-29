package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToolCallRecordsResultAndDuration(t *testing.T) {
	m := New("v-test")
	m.ToolCall("list_series", 120*time.Millisecond, nil)
	m.ToolCall("list_series", 80*time.Millisecond, nil)
	m.ToolCall("get_magnet", time.Second, errors.New("upstream 502"))

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body := rec.Body.String()
	for _, want := range []string{
		`kura_mcp_build_info{version="v-test"} 1`,
		`kura_mcp_tool_calls_total{result="ok",tool="list_series"} 2`,
		`kura_mcp_tool_calls_total{result="error",tool="get_magnet"} 1`,
		`kura_mcp_tool_duration_seconds_count{tool="list_series"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape missing %q", want)
		}
	}
}

func TestNilMetricsIsNoOp(t *testing.T) {
	var m *Metrics
	m.ToolCall("list_series", time.Second, nil)
}
