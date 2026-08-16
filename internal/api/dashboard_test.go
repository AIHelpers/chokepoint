package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleDashboard_ServesHTML(t *testing.T) {
	_, _, mux := newTestAPI(t)
	// dashboard route is registered separately from the JSON API
	a := New(nil, nil, nil)
	a.RegisterDashboard(mux)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %s", ct)
	}
	if !strings.Contains(w.Body.String(), "Chokepoint") {
		t.Error("expected dashboard body to reference Chokepoint")
	}
}
