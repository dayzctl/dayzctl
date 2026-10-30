package webapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles", "test")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pairs := map[string]string{
		"../logs/testdata/sample.ADM": filepath.Join(profiles, "script_2024-03-15_18-02-11.ADM"),
		"../logs/testdata/sample.RPT": filepath.Join(profiles, "script_2024-03-15_17-55-01.RPT"),
	}
	for src, dst := range pairs {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	cfg := &config.ServerConfig{
		Paths: config.Paths{InstallDir: dir},
		Instances: []config.Instance{
			{Name: "test", Port: 2302, Enabled: true},
			{Name: "idle", Port: 2402, Enabled: false},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func do(t *testing.T, srv *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/health", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "ok" {
		t.Errorf("status = %v, want ok", payload["status"])
	}
}

func TestInstancesListsLogCounts(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/instances", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload struct {
		Instances []instanceView `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(payload.Instances))
	}

	test := payload.Instances[0]
	if test.Name != "test" || test.LogFiles != 2 {
		t.Errorf("test instance = %+v, want 2 log files", test)
	}
	if test.Latest == nil {
		t.Error("expected a latest timestamp")
	}

	idle := payload.Instances[1]
	if idle.Error == "" {
		t.Error("expected an error for an instance without a profiles folder")
	}
}

func TestLogsEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/instances/test/logs", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload struct {
		Instance string `json:"instance"`
		Files    []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Instance != "test" {
		t.Errorf("instance = %q", payload.Instance)
	}
	if len(payload.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(payload.Files))
	}
	types := map[string]bool{}
	for _, f := range payload.Files {
		types[f.Type] = true
		if f.Size == 0 {
			t.Errorf("%s has zero size", f.Name)
		}
	}
	if !types["ADM"] || !types["RPT"] {
		t.Errorf("expected ADM and RPT types, got %v", types)
	}
}

func TestAnalyzeEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, http.MethodPost, "/api/instances/test/analyze", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var report struct {
		Instance string `json:"instance"`
		Adm      *struct {
			Players []struct {
				Name  string  `json:"name"`
				Kills int     `json:"kills"`
				KD    float64 `json:"kd"`
			} `json:"players"`
			Totals struct {
				Players  int `json:"players"`
				PvPKills int `json:"pvpKills"`
			} `json:"totals"`
		} `json:"adm"`
		Rpt *struct {
			ProblemCount int `json:"problemCount"`
		} `json:"rpt"`
		RptOrigin string `json:"rptOrigin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if report.Instance != "test" {
		t.Errorf("instance = %q", report.Instance)
	}
	if report.Adm == nil || report.Rpt == nil {
		t.Fatal("expected both ADM and RPT reports")
	}
	if report.Adm.Totals.Players != 4 {
		t.Errorf("players = %d, want 4", report.Adm.Totals.Players)
	}
	if report.Adm.Totals.PvPKills != 2 {
		t.Errorf("pvp kills = %d, want 2", report.Adm.Totals.PvPKills)
	}
	if report.Rpt.ProblemCount != 13 {
		t.Errorf("problem count = %d, want 13", report.Rpt.ProblemCount)
	}
	if report.RptOrigin != "server" {
		t.Errorf("rptOrigin = %q, want server", report.RptOrigin)
	}
}

func TestAnalyzeRespectsFileSelection(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(analyzeRequest{Files: []string{"script_2024-03-15_18-02-11.ADM"}})

	rec := do(t, srv, http.MethodPost, "/api/instances/test/analyze", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var report struct {
		Files []struct {
			Type string `json:"type"`
		} `json:"files"`
		Rpt any `json:"rpt"`
		Adm any `json:"adm"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(report.Files) != 1 || report.Files[0].Type != "ADM" {
		t.Fatalf("expected only the ADM file, got %+v", report.Files)
	}
	if report.Rpt != nil {
		t.Error("expected no RPT section when only an ADM was selected")
	}
	if report.Adm == nil {
		t.Error("expected an ADM section")
	}
}

func TestAnalyzeUnknownInstance(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, http.MethodPost, "/api/instances/nope/analyze", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAnalyzeRejectsTraversal(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(analyzeRequest{Files: []string{"../../../etc/passwd"}})

	rec := do(t, srv, http.MethodPost, "/api/instances/test/analyze", body)
	if rec.Code == http.StatusOK {
		t.Fatal("expected the traversal attempt to be rejected")
	}
}

func TestStaticUIIsServed(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "DayZ Log Analyzer") {
		t.Error("expected the index page to be served")
	}

	// The ES modules must be reachable too.
	jsRec := do(t, srv, http.MethodGet, "/js/api.js", nil)
	if jsRec.Code != http.StatusOK {
		t.Fatalf("js status = %d, want 200", jsRec.Code)
	}
}

func TestGzipWhenAccepted(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("content-encoding = %q, want gzip", got)
	}
}
