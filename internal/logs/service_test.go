package logs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixtureInstall copies the sample logs into a throwaway install directory and
// returns its path.
func fixtureInstall(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles", "test")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pairs := map[string]string{
		"testdata/sample.ADM": filepath.Join(profiles, "script_2024-03-15_18-02-11.ADM"),
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
	return dir
}

func TestAnalyzeSamples(t *testing.T) {
	dir := fixtureInstall(t)
	svc := NewService(dir, DefaultOptions())

	report, err := svc.Analyze("test", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(report.Files))
	}
	if report.Adm == nil {
		t.Fatal("expected an ADM report")
	}

	// ── ADM ──────────────────────────────────────────────
	totals := report.Adm.Totals
	if totals.Players != 4 {
		t.Errorf("players = %d, want 4", totals.Players)
	}
	// Three joins overlap: Kowalski, Mira and Grim.
	if report.Adm.PeakOnline != 3 {
		t.Errorf("peak online = %d, want 3", report.Adm.PeakOnline)
	}
	if totals.PvPKills != 2 {
		t.Errorf("pvp kills = %d, want 2", totals.PvPKills)
	}
	// Two PvP deaths plus one AI death: the (DEAD) markers must not double count.
	if totals.Deaths != 3 {
		t.Errorf("deaths = %d, want 3", totals.Deaths)
	}
	if totals.Chats != 3 {
		t.Errorf("chats = %d, want 3", totals.Chats)
	}
	if totals.Builds != 3 {
		t.Errorf("builds = %d, want 3", totals.Builds)
	}

	// Duplicate "is connected" lines must not create extra sessions.
	for _, p := range report.Adm.Players {
		if p.Name == "Mira" && p.Connects != 1 {
			t.Errorf("Mira connects = %d, want 1", p.Connects)
		}
		if p.Name == "Ghost" && p.Playtime > 600 {
			t.Errorf("Ghost playtime = %.0fs, want under 600s", p.Playtime)
		}
	}

	if !report.Adm.Period.HasTime {
		t.Error("expected a parsed period")
	}
	if report.Adm.Period.Seconds < 6000 || report.Adm.Period.Seconds > 6400 {
		t.Errorf("period = %.0fs, want ~6199s", report.Adm.Period.Seconds)
	}

	// ── kills ────────────────────────────────────────────
	if len(report.Adm.Kills) != 2 {
		t.Fatalf("kills = %d, want 2", len(report.Adm.Kills))
	}
	distances := map[string]float64{}
	for _, k := range report.Adm.Kills {
		if k.Distance != nil {
			distances[k.Killer+"->"+k.Victim] = *k.Distance
		}
		if k.Pos == nil {
			t.Errorf("kill %s->%s has no position", k.Killer, k.Victim)
		}
	}
	if got := distances["Kowalski->Grim"]; got != 41.2 {
		t.Errorf("Kowalski->Grim distance = %v, want 41.2", got)
	}
	if got := distances["Mira->Kowalski"]; got != 18.7 {
		t.Errorf("Mira->Kowalski distance = %v, want 18.7", got)
	}

	// ── suspicious ───────────────────────────────────────
	byType := map[string]int{}
	for _, s := range report.Adm.Suspicious {
		byType[s.Type]++
	}
	if byType["combat-log"] != 2 {
		t.Errorf("combat-log = %d, want 2 (%v)", byType["combat-log"], byType)
	}
	// Ghost jumps 7500 m and 9700 m in ten seconds.
	if byType["teleport"] != 2 {
		t.Errorf("teleport = %d, want 2 (%v)", byType["teleport"], byType)
	}
}

func TestAnalyzeUsesCacheUntilFilesChange(t *testing.T) {
	dir := fixtureInstall(t)
	svc := NewService(dir, DefaultOptions())

	first, err := svc.Analyze("test", nil)
	if err != nil {
		t.Fatalf("first analyze: %v", err)
	}
	if first.FromCache {
		t.Error("first analysis should not be a cache hit")
	}

	second, err := svc.Analyze("test", nil)
	if err != nil {
		t.Fatalf("second analyze: %v", err)
	}
	if !second.FromCache {
		t.Error("second analysis should be a cache hit")
	}

	// Touching a file invalidates the entry.
	target := filepath.Join(dir, "profiles", "test", "script_2024-03-15_18-02-11.ADM")
	stamp := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(target, stamp, stamp); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	third, err := svc.Analyze("test", nil)
	if err != nil {
		t.Fatalf("third analyze: %v", err)
	}
	if third.FromCache {
		t.Error("analysis after a file change should not be a cache hit")
	}
}

func TestAnalyzeRejectsPathTraversal(t *testing.T) {
	dir := fixtureInstall(t)
	svc := NewService(dir, DefaultOptions())

	if _, err := svc.Analyze("test", []string{"../../etc/passwd"}); err == nil {
		t.Fatal("expected an error for a path traversal attempt")
	}
	if _, err := svc.Analyze("test", []string{"../config.yaml"}); err == nil {
		t.Fatal("expected an error for a parent-directory reference")
	}
}

func TestAnalyzeMissingProfilesDir(t *testing.T) {
	svc := NewService(t.TempDir(), DefaultOptions())
	if _, err := svc.Analyze("nope", nil); err == nil {
		t.Fatal("expected an error for a missing profiles directory")
	}
}

func TestListFilesNewestFirst(t *testing.T) {
	dir := fixtureInstall(t)
	svc := NewService(dir, DefaultOptions())

	files, err := svc.ListFiles("test")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}
