package webhookserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// fakeServer is a minimal webhook.Server that records the handler registered
// for each path, so the wrapper can be tested without TLS.
type fakeServer struct {
	webhook.Server
	handlers map[string]http.Handler
}

func (f *fakeServer) Register(path string, hook http.Handler) {
	if f.handlers == nil {
		f.handlers = map[string]http.Handler{}
	}
	f.handlers[path] = hook
}

func TestWithSecurityHeadersSetsHeadersOnRegisteredHandlers(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantStatus  int
		wantBody    string
		wantHeaders map[string]string
	}{
		{
			name: "successful response carries both headers",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("{}"))
			},
			wantStatus: http.StatusOK,
			wantBody:   "{}",
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=31536000",
				"X-Content-Type-Options":    "nosniff",
			},
		},
		{
			name: "error response carries both headers",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "bad request", http.StatusBadRequest)
			},
			wantStatus: http.StatusBadRequest,
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=31536000",
				"X-Content-Type-Options":    "nosniff",
			},
		},
		{
			name:       "no-op handler still carries both headers",
			handler:    func(w http.ResponseWriter, r *http.Request) {},
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Strict-Transport-Security": "max-age=31536000",
				"X-Content-Type-Options":    "nosniff",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeServer{}
			server := WithSecurityHeaders(fake)

			const path = "/test-webhook"
			server.Register(path, tt.handler)

			registered, ok := fake.handlers[path]
			if !ok {
				t.Fatalf("handler was not registered on the wrapped server for path %q", path)
			}

			req := httptest.NewRequest(http.MethodPost, path, nil)
			rec := httptest.NewRecorder()
			registered.ServeHTTP(rec, req)

			res := rec.Result()
			if res.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			for header, want := range tt.wantHeaders {
				if got := res.Header.Get(header); got != want {
					t.Errorf("header %s = %q, want %q", header, got, want)
				}
			}
		})
	}
}

func TestWithSecurityHeadersPreservesBodyAndContentType(t *testing.T) {
	fake := &fakeServer{}
	server := WithSecurityHeaders(fake)

	const path = "/convert"
	server.Register(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"converted":true}`))
	}))

	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	fake.handlers[path].ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if got := res.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if rec.Body.String() != `{"converted":true}` {
		t.Errorf("body = %q, want %q", rec.Body.String(), `{"converted":true}`)
	}
	if got := res.Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("Strict-Transport-Security = %q, want %q", got, "max-age=31536000")
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}
