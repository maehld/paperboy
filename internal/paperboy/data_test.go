package paperboy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// dataCall performs a request against the server with the upload token.
func dataCall(t *testing.T, s *Server, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func readJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("%v in %s", err, body)
	}
	return v
}

func TestDataEndpointsRequireToken(t *testing.T) {
	s, _ := testServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/data/solar", `{"a":1}`},
		{http.MethodGet, "/api/data", ""},
		{http.MethodDelete, "/api/data/solar", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestDataPutReplacesSubtree(t *testing.T) {
	s, _ := testServer(t)
	if code, body := dataCall(t, s, http.MethodPut, "/api/data/solar/current", `{"pv_power":4210,"load_power":820}`); code != http.StatusNoContent {
		t.Fatalf("put: %d %s", code, body)
	}
	// PUT does not merge: the whole branch is replaced.
	if code, body := dataCall(t, s, http.MethodPut, "/api/data/solar/current", `{"pv_power":1200}`); code != http.StatusNoContent {
		t.Fatalf("replace: %d %s", code, body)
	}
	code, body := dataCall(t, s, http.MethodGet, "/api/data/solar/current", "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	values := readJSON[map[string]any](t, body)
	if len(values) != 1 || values["pv_power"] != 1200.0 {
		t.Fatalf("PUT must replace, not merge: %v", values)
	}
}

func TestDataDeepPutCreatesObjects(t *testing.T) {
	s, _ := testServer(t)
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/wetter/aktuell/temp", `18.4`); code != http.StatusNoContent {
		t.Fatalf("deep put: %d", code)
	}
	code, body := dataCall(t, s, http.MethodGet, "/api/data/wetter", "")
	if code != http.StatusOK {
		t.Fatalf("get: %d %s", code, body)
	}
	wetter := readJSON[map[string]any](t, body)
	if _, ok := wetter["aktuell"].(map[string]any); !ok {
		t.Fatalf("intermediate object missing: %v", wetter)
	}
}

func TestDataConflict(t *testing.T) {
	s, _ := testServer(t)
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/solar/current", `5`); code != http.StatusNoContent {
		t.Fatalf("scalar put: %d", code)
	}
	if code, body := dataCall(t, s, http.MethodPut, "/api/data/solar/current/latest", `1`); code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", code, body)
	}
}

func TestDataDelete(t *testing.T) {
	s, _ := testServer(t)
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/solar/daily/points", `[1,2]`); code != http.StatusNoContent {
		t.Fatalf("put: %d", code)
	}
	if code, _ := dataCall(t, s, http.MethodDelete, "/api/data/solar/daily", ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := dataCall(t, s, http.MethodGet, "/api/data/solar/daily", ""); code != http.StatusNotFound {
		t.Fatalf("deleted branch must be gone: %d", code)
	}
	if code, _ := dataCall(t, s, http.MethodDelete, "/api/data/solar/daily", ""); code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/Solar", `1`); code != http.StatusBadRequest {
		t.Fatalf("invalid path: %d", code)
	}
}

func TestDataRootReplace(t *testing.T) {
	s, _ := testServer(t)
	dataCall(t, s, http.MethodPut, "/api/data/solar", `{"current":1}`)
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data", `{"wetter":{"text":"Sonnig"}}`); code != http.StatusNoContent {
		t.Fatalf("root put: %d", code)
	}
	if code, _ := dataCall(t, s, http.MethodGet, "/api/data/solar", ""); code != http.StatusNotFound {
		t.Fatalf("root replace must drop other branches: %d", code)
	}
	code, body := dataCall(t, s, http.MethodGet, "/api/data", "")
	if code != http.StatusOK {
		t.Fatalf("get root: %d", code)
	}
	if root := readJSON[map[string]any](t, body); root["wetter"] == nil {
		t.Fatalf("root content: %v", root)
	}
}

func TestDataSupplierTime(t *testing.T) {
	s, now := testServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/data/solar", strings.NewReader(`{"current":1}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Data-Time", now.Add(-10*time.Minute).Format(time.RFC3339))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("put: %d", rec.Code)
	}
	if age, ok := s.doc.Age("solar/current"); !ok || age != 10*time.Minute {
		t.Fatalf("supplier time not used: %v, %t", age, ok)
	}

	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/solar", `1`); code != http.StatusNoContent {
		t.Fatalf("put without header: %d", code)
	}
	bad := httptest.NewRequest(http.MethodPut, "/api/data/solar", strings.NewReader(`1`))
	bad.Header.Set("Authorization", "Bearer "+testToken)
	bad.Header.Set("X-Data-Time", "gestern")
	bc := httptest.NewRecorder()
	s.Handler().ServeHTTP(bc, bad)
	if bc.Code != http.StatusBadRequest {
		t.Fatalf("invalid X-Data-Time: %d", bc.Code)
	}
}

func TestDataRejectsOversizedBody(t *testing.T) {
	s, _ := testServer(t)
	body := strings.Repeat("1", maxDataBytes+10)
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/solar", body); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", code)
	}
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/solar", `{"a":`); code != http.StatusBadRequest {
		t.Fatalf("invalid JSON: %d", code)
	}
}

func TestDataSurvivesRestart(t *testing.T) {
	s, _ := testServer(t)
	if code, _ := dataCall(t, s, http.MethodPut, "/api/data/solar/current", `{"pv_power":4210}`); code != http.StatusNoContent {
		t.Fatalf("put: %d", code)
	}
	restarted, err := New(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	code, body := dataCall(t, restarted, http.MethodGet, "/api/data/solar/current/pv_power", "")
	if code != http.StatusOK {
		t.Fatalf("get after restart: %d %s", code, body)
	}
	if v := readJSON[float64](t, body); v != 4210 {
		t.Fatalf("value after restart: %v", v)
	}
}
