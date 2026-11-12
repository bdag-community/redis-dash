package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemovedStatusTabsReturnNotFound(t *testing.T) {
	app := &App{templates: parseTemplates()}
	for _, path := range []string{"/tabs/node-sync", "/tabs/pool-status"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		app.handleTab(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
	}
}
