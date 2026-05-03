package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/api/handlers"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/gin-gonic/gin"
)

// fakeURLService implements service.URLService with per-method function fields.
// Mirrors the fakeURLRepo pattern in internal/service/fakes_test.go so the
// handler tests can shape any response/error per case without a real DB.
type fakeURLService struct {
	ShortenURLFn func(ctx context.Context, url, ua string) (*model.URL, error)
	RedirectFn   func(ctx context.Context, key string) (string, error)
}

func (f *fakeURLService) ShortenURL(ctx context.Context, url, ua string) (*model.URL, error) {
	return f.ShortenURLFn(ctx, url, ua)
}

func (f *fakeURLService) Redirect(ctx context.Context, key string) (string, error) {
	return f.RedirectFn(ctx, key)
}

// newRouter wires the URLHandler under a fresh gin.Engine using only the routes
// the handler owns — middleware is intentionally absent so each test exercises
// the handler in isolation.
func newRouter(svc *fakeURLService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handlers.URLHandler{Service: svc}
	r.POST("/shorten", h.Create)
	r.GET("/:key", h.Redirect)
	return r
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var bodyReader *bytes.Buffer
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	} else {
		bodyReader = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ---------- Create ----------

func TestCreate_HappyPath_201(t *testing.T) {
	t.Parallel()
	wantURL := "https://example.com/some/long/path"
	svc := &fakeURLService{
		ShortenURLFn: func(_ context.Context, url, _ string) (*model.URL, error) {
			if url != wantURL {
				t.Errorf("service got url %q, want %q", url, wantURL)
			}
			u := model.NewURL(url, "short_key01")
			return &u, nil
		},
	}
	rec := do(newRouter(svc), http.MethodPost, "/shorten", `{"url":"`+wantURL+`"}`)

	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d want 201, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Errorf("body should be success envelope, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"short_key":"short_key01"`) {
		t.Errorf("body missing short_key, got %s", rec.Body.String())
	}
}

func TestCreate_BadJSON_400(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		ShortenURLFn: func(context.Context, string, string) (*model.URL, error) {
			t.Error("service should not be called on invalid input")
			return nil, nil
		},
	}
	rec := do(newRouter(svc), http.MethodPost, "/shorten", `{not even json`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalid_input"`) {
		t.Errorf("body should carry invalid_input, got %s", rec.Body.String())
	}
}

func TestCreate_MissingURLField_400(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		ShortenURLFn: func(context.Context, string, string) (*model.URL, error) {
			t.Error("service should not be called when validation fails")
			return nil, nil
		},
	}
	rec := do(newRouter(svc), http.MethodPost, "/shorten", `{}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreate_NotAURL_400(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		ShortenURLFn: func(context.Context, string, string) (*model.URL, error) {
			t.Error("service should not be called when URL fails the binding tag")
			return nil, nil
		},
	}
	rec := do(newRouter(svc), http.MethodPost, "/shorten", `{"url":"not-a-url"}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
}

func TestCreate_ServiceError_500(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		ShortenURLFn: func(context.Context, string, string) (*model.URL, error) {
			return nil, errors.New("disk full")
		},
	}
	rec := do(newRouter(svc), http.MethodPost, "/shorten", `{"url":"https://example.com"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"internal_error"`) {
		t.Errorf("body should carry internal_error code, got %s", rec.Body.String())
	}
}

func TestCreate_ContextDeadlineExceeded_504(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		ShortenURLFn: func(context.Context, string, string) (*model.URL, error) {
			return nil, context.DeadlineExceeded
		},
	}
	rec := do(newRouter(svc), http.MethodPost, "/shorten", `{"url":"https://example.com"}`)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status: got %d want 504", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"timeout"`) {
		t.Errorf("body should carry timeout code, got %s", rec.Body.String())
	}
}

// ---------- Redirect ----------

func TestRedirect_HappyPath_301(t *testing.T) {
	t.Parallel()
	want := "https://example.com/destination"
	svc := &fakeURLService{
		RedirectFn: func(_ context.Context, key string) (string, error) {
			if key != "abc12345xyz" {
				t.Errorf("service got key %q, want %q", key, "abc12345xyz")
			}
			return want, nil
		},
	}
	rec := do(newRouter(svc), http.MethodGet, "/abc12345xyz", "")

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status: got %d want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location: got %q want %q", got, want)
	}
}

func TestRedirect_NotFound_404(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		RedirectFn: func(context.Context, string) (string, error) {
			return "", repository.ErrNotFound
		},
	}
	rec := do(newRouter(svc), http.MethodGet, "/missing", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("body should carry not_found code, got %s", rec.Body.String())
	}
}

func TestRedirect_ServiceError_500(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		RedirectFn: func(context.Context, string) (string, error) {
			return "", errors.New("conn refused")
		},
	}
	rec := do(newRouter(svc), http.MethodGet, "/somekey", "")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d want 500", rec.Code)
	}
}

func TestRedirect_DeadlineExceeded_504(t *testing.T) {
	t.Parallel()
	svc := &fakeURLService{
		RedirectFn: func(context.Context, string) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	rec := do(newRouter(svc), http.MethodGet, "/somekey", "")

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status: got %d want 504", rec.Code)
	}
}
