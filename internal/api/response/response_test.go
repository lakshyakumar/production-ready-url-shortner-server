package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/response"

	"github.com/gin-gonic/gin"
)

// runHandler exercises a one-off handler against the response package and
// returns the recorder so each test can assert on the body / status / headers.
func runHandler(fn gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", fn)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	return rec
}

func TestOK_WrapsDataInSuccessEnvelope(t *testing.T) {
	t.Parallel()
	rec := runHandler(func(c *gin.Context) {
		response.OK(c, http.StatusCreated, gin.H{"id": "abc"})
	})

	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d want 201", rec.Code)
	}
	var got response.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Success {
		t.Error("Success: got false, want true")
	}
	if got.Error != nil {
		t.Errorf("Error: got %+v, want nil", got.Error)
	}
	// Data is interface{}, so just round-trip check the field is non-nil.
	if got.Data == nil {
		t.Error("Data is nil; expected payload")
	}
}

func TestError_WrapsCodeAndMessageInFailureEnvelope(t *testing.T) {
	t.Parallel()
	rec := runHandler(func(c *gin.Context) {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidInput, "bad url")
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
	var got response.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Success {
		t.Error("Success: got true, want false")
	}
	if got.Error == nil {
		t.Fatal("Error is nil; expected APIError")
	}
	if got.Error.Code != response.CodeInvalidInput {
		t.Errorf("Code: got %q want %q", got.Error.Code, response.CodeInvalidInput)
	}
	if got.Error.Message != "bad url" {
		t.Errorf("Message: got %q want %q", got.Error.Message, "bad url")
	}
}

// TestErrorCodes_AreStable guards against accidental rename of the canonical
// error codes — clients match on these strings, so changing them is a
// compatibility break.
func TestErrorCodes_AreStable(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"invalid_input":  response.CodeInvalidInput,
		"not_found":      response.CodeNotFound,
		"unavailable":    response.CodeUnavailable,
		"internal_error": response.CodeInternal,
		"timeout":        response.CodeTimeout,
		"rate_limited":   response.CodeRateLimited,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("code drift: got %q want %q", got, want)
		}
	}
}
