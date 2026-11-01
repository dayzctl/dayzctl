package logs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestProbeRealLogs analyses a real profiles folder for manual inspection.
//
// It is skipped unless DAYZCTL_REAL_LOGS points at a directory containing real
// .ADM/.RPT files, so the normal suite stays self-contained and fast:
//
//	DAYZCTL_REAL_LOGS=~/Downloads go test ./internal/logs -run TestProbeRealLogs -v
func TestProbeRealLogs(t *testing.T) {
	src := os.Getenv("DAYZCTL_REAL_LOGS")
	if src == "" {
		t.Skip("set DAYZCTL_REAL_LOGS to a folder with real .ADM/.RPT files")
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}

	install := t.TempDir()
	profiles := filepath.Join(install, "profiles", "real")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	copied := 0
	for _, entry := range entries {
		if entry.IsDir() || !IsCandidateEntry(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(profiles, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", entry.Name(), err)
		}
		copied++
	}
	if copied == 0 {
		t.Fatalf("no log files in %s", src)
	}

	svc := NewService(install, DefaultOptions())
	started := time.Now()
	report, err := svc.Analyze("real", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	elapsed := time.Since(started)

	t.Logf("analysed %d file(s) in %s", len(report.Files), elapsed.Round(time.Millisecond))
	for _, f := range report.Files {
		t.Logf("  %-42s %-6s %8d lines %6.1f MB %s", f.Name, f.Type, f.Lines, float64(f.Bytes)/1e6, f.Error)
	}
	t.Logf("adm events=%d unmatched=%d", report.Stats.AdmEvents, report.Stats.AdmUnmatched)

	if adm := report.Adm; adm != nil {
		t.Logf("ADM totals: %+v", adm.Totals)
		t.Logf("ADM peak online=%d period=%.0fs (%v -> %v)", adm.PeakOnline, adm.Period.Seconds, adm.Period.From, adm.Period.To)
		for _, p := range adm.Players {
			t.Logf("  player %-24s sessions=%d playtime=%.0f kills=%d deaths=%d pvpD=%d aiD=%d chats=%d builds=%d hitsTaken=%d flags=%d",
				p.Name, len(p.Sessions), p.Playtime, p.Kills, p.Deaths, p.PvPDeaths, p.AiDeaths, p.Chats, p.Builds, p.HitsTaken, p.Suspicion)
		}
		for i, k := range adm.Kills {
			if i >= 10 {
				break
			}
			d := -1.0
			if k.Distance != nil {
				d = *k.Distance
			}
			t.Logf("  kill %-20s -> %-20s weapon=%-12q dist=%.1f pos=%v", k.Killer, k.Victim, k.Weapon, d, k.Pos)
		}
		types := map[string]int{}
		for _, s := range adm.Suspicious {
			types[s.Type]++
		}
		t.Logf("  suspicious: %v", types)
	}

	if rpt := report.Rpt; rpt != nil {
		t.Logf("RPT counts: %+v problems=%d groups=%d end=%s origin=%s", rpt.Counts, rpt.ProblemCount, rpt.GroupCount, rpt.EndState, report.RptOrigin)
		groups := append([]RptGroup(nil), rpt.Groups...)
		sort.SliceStable(groups, func(i, j int) bool { return groups[i].Count > groups[j].Count })
		for i, g := range groups {
			if i >= 20 {
				break
			}
			t.Logf("  %-8s %8dx %-58s %q", g.Severity, g.Count, truncateStr(g.Title, 58), truncateStr(g.Sample, 70))
		}
	}

	// Surface the ADM lines the parser does not understand, so gaps are visible.
	if len(report.Files) > 0 {
		for _, f := range report.Files {
			if f.Type != FileADM {
				continue
			}
			unmatched := sampleUnmatched(filepath.Join(profiles, f.Name))
			for i, line := range unmatched {
				if i >= 12 {
					break
				}
				t.Logf("  unmatched ADM: %s", truncateStr(line, 130))
			}
		}
	}
}

func truncateStr(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-1] + "…"
}

// sampleUnmatched returns distinct ADM messages the parser classified as OTHER.
func sampleUnmatched(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	parser := &AdmParser{}
	seen := map[string]bool{}
	out := []string{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineLen)
	for sc.Scan() {
		line := sc.Text()
		before := parser.Unmatched
		parser.Feed(line)
		if parser.Unmatched > before {
			msg := line
			if ts, ok := ParseTimestamp(line); ok {
				msg = ts.Rest
			}
			key := normalizeUnmatched(msg)
			if !seen[key] {
				seen[key] = true
				out = append(out, msg)
			}
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Printf("scan %s: %v\n", path, err)
	}
	return out
}

func normalizeUnmatched(s string) string {
	if len(s) > 60 {
		return s[:60]
	}
	return fmt.Sprint(s)
}
