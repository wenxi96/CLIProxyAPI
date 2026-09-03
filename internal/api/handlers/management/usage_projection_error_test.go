package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

func TestWriteUsageProjectionBudgetErrorKeepsUnavailableMachineCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeUsageProjectionError(context, &usage.ProjectionBudgetError{
		Dimension: "scanned_fact_rows",
		Limit:     10,
		Observed:  11,
	})

	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("response = status:%d headers:%+v body:%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["error"] != "projection_unavailable" || payload["code"] != "projection_unavailable" ||
		payload["projection_state"] != "budget_exceeded" || payload["budget_dimension"] != "scanned_fact_rows" {
		t.Fatalf("budget response = %#v", payload)
	}
}
