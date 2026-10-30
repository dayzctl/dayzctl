package logs

import (
	"strings"
	"testing"
)

func TestParseTimestampFormats(t *testing.T) {
	cases := []struct {
		line     string
		wantSecs float64
		wantDate bool
		wantRest string
	}{
		{
			line:     `18:22:31 | Player "Bob" is connected`,
			wantSecs: 18*3600 + 22*60 + 31,
			wantDate: false,
			wantRest: `| Player "Bob" is connected`,
		},
		{
			line:     `2024-03-15 18:22:31 | Player "Bob" is connected`,
			wantSecs: 18*3600 + 22*60 + 31,
			wantDate: true,
			wantRest: `| Player "Bob" is connected`,
		},
		{
			line:     ` 0:00:09 Mission read.`,
			wantSecs: 9,
			wantDate: false,
			wantRest: `Mission read.`,
		},
		{
			line:     `[19:02:11] Player "Ghost" is connected`,
			wantSecs: 19*3600 + 2*60 + 11,
			wantDate: false,
			wantRest: `Player "Ghost" is connected`,
		},
	}

	for _, tc := range cases {
		ts, ok := ParseTimestamp(tc.line)
		if !ok {
			t.Fatalf("%q: expected a timestamp", tc.line)
		}
		if ts.Secs != tc.wantSecs {
			t.Errorf("%q: secs = %v, want %v", tc.line, ts.Secs, tc.wantSecs)
		}
		if (ts.Day != nil) != tc.wantDate {
			t.Errorf("%q: hasDate = %v, want %v", tc.line, ts.Day != nil, tc.wantDate)
		}
		if strings.TrimSpace(ts.Rest) != tc.wantRest {
			t.Errorf("%q: rest = %q, want %q", tc.line, strings.TrimSpace(ts.Rest), tc.wantRest)
		}
	}
}

func TestParseTimestampRejectsNonClock(t *testing.T) {
	if _, ok := ParseTimestamp(`Class 3:4 not found`); ok {
		t.Fatal("expected no timestamp for a non-clock prefix")
	}
}

func TestClockRollsOverMidnight(t *testing.T) {
	var c Clock
	if got := c.Stamp(23*3600+59*60, nil); got != 23*3600+59*60 {
		t.Fatalf("first stamp = %v", got)
	}
	got := c.Stamp(60, nil) // 00:01 the next day
	want := 86400 + 60.0
	if got != want {
		t.Fatalf("after midnight = %v, want %v", got, want)
	}
}

func TestClockKeepsOutOfOrderLinesOnSameDay(t *testing.T) {
	var c Clock
	c.Stamp(10*3600, nil)
	got := c.Stamp(9*3600, nil) // small backwards jump, same day
	if got != 9*3600 {
		t.Fatalf("out-of-order stamp = %v, want %v", got, 9*3600.0)
	}
}

func TestParseAdmMessage(t *testing.T) {
	cases := []struct {
		msg  string
		kind string
	}{
		{`Player "Bob" is connected (id=12345)`, KindConnect},
		{`Player "Bob" (id=12345 pos=<1.0, 2.0, 3.0>) is connected`, KindConnect},
		{`Player "Bob" disconnected`, KindDisconnect},
		{`Player "Bob" killed by Player "Alice" with M4A1 from 41.2 m`, KindKill},
		{`Player "Bob" (DEAD) (id=1 pos=<1.0, 2.0, 3.0>)`, KindDeath},
		{`Player "Bob" killed by Infected from 2.4 m`, KindDeath},
		{`Player "Bob" hit by Player "Alice" with AK74 into LeftLeg(19)`, KindHit},
		{`Player "Bob" said in chat: hi there`, KindChat},
		{`Player "Bob" built Wooden Wall at <4885.0, 9462.3, 318.0>`, KindBuild},
		{`Player "Bob" has been kicked by BattlEye`, KindKick},
		{`Player "Bob" has been banned`, KindBan},
		{`Player "Bob" (id=1 pos=<10.0, 20.0, 30.0>)`, KindPosition},
		{`Something else entirely`, KindOther},
	}

	for _, tc := range cases {
		if got := ParseAdmMessage(tc.msg).Kind; got != tc.kind {
			t.Errorf("%q: kind = %s, want %s", tc.msg, got, tc.kind)
		}
	}
}

func TestParseAdmMessageExtractsDetails(t *testing.T) {
	kill := ParseAdmMessage(`Player "Grim" killed by Player "Kowalski" with M4A1 from 41.2 m`)
	if kill.Weapon != "M4A1" {
		t.Errorf("weapon = %q, want M4A1", kill.Weapon)
	}
	if kill.Distance == nil || *kill.Distance != 41.2 {
		t.Errorf("distance = %v, want 41.2", kill.Distance)
	}
	if kill.Player != "Grim" || kill.Other != "Kowalski" {
		t.Errorf("victim/killer = %q/%q", kill.Player, kill.Other)
	}

	hit := ParseAdmMessage(`Player "Grim" hit by Player "Kowalski" with M4A1 into LeftArm(28)`)
	if hit.Hitbox != "LeftArm" {
		t.Errorf("hitbox = %q", hit.Hitbox)
	}
	if hit.Damage == nil || *hit.Damage != 28 {
		t.Errorf("damage = %v, want 28", hit.Damage)
	}

	build := ParseAdmMessage(`Player "Mira" built Fence Kit at <11218.0, 5325.0, 210.0>`)
	if build.Text != "Fence Kit" {
		t.Errorf("build text = %q, want %q", build.Text, "Fence Kit")
	}
}

func TestParseAdmMessagePositionUsesXYZOrder(t *testing.T) {
	event := Event{}
	parser := &AdmParser{}
	parser.Feed(`19:02:23 | Player "Ghost" (id=42 pos=<12000.0, 12000.0, 300.0>)`)

	if len(parser.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(parser.Events))
	}
	event = parser.Events[0]
	if event.Pos == nil {
		t.Fatal("expected a position")
	}
	// pos=<x, y, z> is east, north, altitude.
	if event.Pos.X != 12000 || event.Pos.Y != 12000 || event.Pos.Z != 300 {
		t.Fatalf("pos = %+v", *event.Pos)
	}
}

func TestDetectType(t *testing.T) {
	cases := []struct {
		name   string
		sample []string
		want   FileType
	}{
		{"script_2024.ADM", nil, FileADM},
		{"script_2024.RPT", nil, FileRPT},
		{"crash.mdmp", nil, FileCrash},
		{"unknown.log", []string{`18:02:11 | Player "Bob" is connected (id=1)`}, FileADM},
		// A generic "Warning:" line must NOT turn an unrelated .log into an RPT.
		{"unknown.log", []string{`17:29:58 Warning: something broke`}, FileUnknown},
		{"unknown.log", []string{`ERROR: nvidia-installer failed`}, FileUnknown},
		// Real DayZ server RPT markers.
		{"unknown.log", []string{`Exe timestamp: 2026/10/21 11:43:16`, `Average server FPS: 612.00 (measured interval: 1 s)`}, FileRPT},
		{"unknown.log", []string{`10:00:00    ENTITY    (W): Door 'door1' is missing geometry components`}, FileRPT},
		{"unknown.log", []string{`SCRIPT       : (E): Error in expression`}, FileScript},
	}
	for _, tc := range cases {
		if got := DetectType(tc.name, tc.sample); got != tc.want {
			t.Errorf("DetectType(%q) = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestIsCandidateEntry(t *testing.T) {
	yes := []string{"script_1.ADM", "script_1.RPT", "script.log", "notes.txt", "crash.mdmp"}
	no := []string{"", "dir/", ".hidden", "storage_1.bin", "serverDZ.cfg"}

	for _, name := range yes {
		if !IsCandidateEntry(name) {
			t.Errorf("%q should be a candidate", name)
		}
	}
	for _, name := range no {
		if IsCandidateEntry(name) {
			t.Errorf("%q should not be a candidate", name)
		}
	}
}

func TestNormalizeMessageCollapsesVolatileParts(t *testing.T) {
	a := NormalizeMessage(`Cannot open object mymod\objects\wall1.p3d`)
	b := NormalizeMessage(`Cannot open object mymod\objects\wall2.p3d`)

	if a != b {
		t.Fatalf("expected the same group key:\n%s\n%s", a, b)
	}
	if !strings.Contains(a, "<path>") {
		t.Errorf("expected a <path> placeholder, got %q", a)
	}
}
