package compression

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware(t *testing.T) {
	cfg := Config{GzipLevel: 6}
	mw := Middleware(cfg)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))

	// Client without Accept-Encoding: no compression
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("unexpected Content-Encoding when client doesn't accept gzip: %s", rec.Header().Get("Content-Encoding"))
	}
	if rec.Body.String() != "hello world" {
		t.Errorf("body: got %q", rec.Body.String())
	}

	// Client with Accept-Encoding: gzip
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %s", rec2.Header().Get("Content-Encoding"))
	}
	if rec2.Body.Len() == 0 {
		t.Error("expected non-empty gzip body")
	}
	// Body should be gzip compressed (starts with gzip magic)
	body := rec2.Body.Bytes()
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		t.Errorf("expected gzip magic, got %x", body[:min(4, len(body))])
	}
}

func TestAcceptsGzip(t *testing.T) {
	tests := []struct {
		ae     string
		accept bool
	}{
		{"gzip", true},
		{"GZIP", true},
		{"gzip, deflate, br", true},
		{"deflate, br", false},
		{"", false},
		{"identity", false},
		{"gzip;q=1.0", true},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", tt.ae)
		got := acceptsGzip(r)
		if got != tt.accept {
			t.Errorf("acceptsGzip(%q) = %v, want %v", tt.ae, got, tt.accept)
		}
	}
}

func TestLevel(t *testing.T) {
	if Level(0) != 1 {
		t.Errorf("Level(0) = %d, want 1", Level(0))
	}
	if Level(10) != 9 {
		t.Errorf("Level(10) = %d, want 9", Level(10))
	}
	if Level(5) != 5 {
		t.Errorf("Level(5) = %d, want 5", Level(5))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
