package main

import (
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/health", nil))
	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("health: %d %s", rec.Code, rec.Body)
	}
}
