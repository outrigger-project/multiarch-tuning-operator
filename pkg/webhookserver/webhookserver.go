package webhookserver

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	strictTransportSecurityHeader = "Strict-Transport-Security"
	// includeSubDomains is omitted: the service DNS name is not a parent
	// domain we control, and the header is irrelevant to the API server client.
	strictTransportSecurityValue = "max-age=31536000"
	xContentTypeOptionsHeader    = "X-Content-Type-Options"
	xContentTypeOptionsValue     = "nosniff"
)

// WithSecurityHeaders wraps s so every registered handler gets the security
func WithSecurityHeaders(s webhook.Server) webhook.Server {
	return &securityHeadersServer{Server: s}
}

type securityHeadersServer struct {
	webhook.Server
}

func (s *securityHeadersServer) Register(path string, hook http.Handler) {
	s.Server.Register(path, withSecurityHeaders(hook))
}

// withSecurityHeaders sets the headers before next writes the response, so
// they are present on every response, including error responses.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set(strictTransportSecurityHeader, strictTransportSecurityValue)
		h.Set(xContentTypeOptionsHeader, xContentTypeOptionsValue)
		next.ServeHTTP(w, r)
	})
}
