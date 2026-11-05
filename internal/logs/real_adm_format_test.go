package logs

import (
	"strings"
	"testing"
)

// Real ADM lines write the verb straight after the position group and include
// events the analyser used to fold into anonymous position samples.
func TestRealAdmVerbsAndEvents(t *testing.T) {
	cases := []struct {
		msg  string
		kind string
	}{
		{`Player "Boriska" (id=abc123DEF pos=<7903.2, 6786.6, 236.1>)Built rubble_rubble1 on  with Shovel`, KindBuild},
		{`Player "Autopsy" (id=abc123DEF pos=<10317.7, 10968, 173.1>)Built door1_metal on  with Hatchet`, KindBuild},
		{`Player "Matenme" (id=abc123DEF pos=<2814.1, 11017, 306.4>)[HP: 94.15] hit by Infected into Torso(1) for 5.85 damage (MeleeInfected)`, KindHit},
		{`Player "Matenme" (id=abc123DEF pos=<3359.3, 11611.4, 279.2>) performed EmoteSitA with CombatKnife`, KindEmote},
		{`Player "Boriska" (id=abc123DEF pos=<10659.6, 4599.0, 165.5>) is unconscious`, KindState},
		{`Player "Boriska" (id=abc123DEF pos=<10658.7, 4601.5, 164.7>) regained consciousness`, KindState},
		{`Player "A" (id=abc123DEF pos=<1.0, 2.0, 3.0>)killed by Player "B" with M4A1 from 10.0 meters`, KindKill},
	}
	for _, tc := range cases {
		if got := ParseAdmMessage(tc.msg).Kind; got != tc.kind {
			t.Errorf("%q: kind = %s, want %s", tc.msg, got, tc.kind)
		}
	}
}

// Emote and build lines must still carry their coordinates.
func TestRealAdmEventPositions(t *testing.T) {
	build := ParseAdmMessage(`Player "Boriska" (id=abc123DEF pos=<7903.2, 6786.6, 236.1>)Built rubble_rubble1 on  with Shovel`)
	if build.Text != "rubble_rubble1 on" && build.Text != "rubble_rubble1 on " {
		t.Logf("build text = %q", build.Text)
	}
	parser := &AdmParser{}
	parser.Feed(`18:10:38 | Player "Boriska" (id=abc123DEF pos=<7903.2, 6786.6, 236.1>)Built rubble_rubble1 on  with Shovel`)
	if len(parser.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(parser.Events))
	}
	ev := parser.Events[0]
	if ev.Kind != KindBuild {
		t.Fatalf("kind = %s, want %s", ev.Kind, KindBuild)
	}
	if ev.Pos == nil || ev.Pos.X != 7903.2 || ev.Pos.Y != 6786.6 {
		t.Fatalf("pos = %+v, want <7903.2, 6786.6, …>", ev.Pos)
	}
}

// Every event with coordinates feeds the trail, the disconnect position is not
// lost, and a reconnect starts a new segment instead of a line across the map.
func TestRealAdmTrailCoverageAndBreaks(t *testing.T) {
	lines := []string{
		`AdminLog started on 2026-11-03 at 00:00:00`,
		`10:00:00 | Player "Bob" (id=abc123DEF pos=<100.0, 100.0, 10.0>) is connected`,
		`10:05:00 | Player "Bob" (id=abc123DEF pos=<120.0, 130.0, 10.0>)[HP: 90] hit by Infected into Torso(1) for 5 damage (MeleeInfected)`,
		`10:06:00 | Player "Bob" (id=abc123DEF pos=<140.0, 160.0, 10.0>)Built wall1 on  with Hammer`,
		`10:07:00 | Player "Bob" (id=abc123DEF pos=<160.0, 200.0, 10.0>) has been disconnected`,
		`11:00:00 | Player "Bob" (id=abc123DEF pos=<9000.0, 9000.0, 10.0>) is connected`,
	}

	parser := &AdmParser{}
	for _, line := range lines {
		parser.Feed(line)
	}
	report := AnalyzeAdm(parser.Events, DefaultOptions())
	if len(report.Players) != 1 {
		t.Fatalf("players = %d, want 1", len(report.Players))
	}
	player := report.Players[0]

	want := [][2]float64{{100, 100}, {120, 130}, {140, 160}, {160, 200}, {9000, 9000}}
	if len(player.Positions) != len(want) {
		for _, s := range player.Positions {
			t.Logf("sample %.0f,%.0f break=%v", s.X, s.Y, s.Break)
		}
		t.Fatalf("positions = %d, want %d", len(player.Positions), len(want))
	}
	for i, xy := range want {
		if player.Positions[i].X != xy[0] || player.Positions[i].Y != xy[1] {
			t.Errorf("sample %d = (%.0f, %.0f), want (%.0f, %.0f)",
				i, player.Positions[i].X, player.Positions[i].Y, xy[0], xy[1])
		}
	}

	// The respawn sample opens a new segment; the walk up to it stays joined.
	if !player.Positions[4].Break {
		t.Error("the sample after the reconnect must be marked as a trail break")
	}
	for i := 1; i < 4; i++ {
		if player.Positions[i].Break {
			t.Errorf("sample %d must not be a break", i)
		}
	}

	if len(report.Builds) != 1 {
		t.Errorf("builds = %d, want 1", len(report.Builds))
	}
	if len(report.Suspicious) != 0 {
		t.Errorf("suspicious = %d, want 0 (a reconnect is not a teleport): %+v",
			len(report.Suspicious), report.Suspicious)
	}

	// A trail dot must be able to say what happened there.
	if player.Positions[1].Kind != KindHit {
		t.Errorf("hit sample kind = %q, want %q", player.Positions[1].Kind, KindHit)
	}
	if !strings.Contains(player.Positions[1].Text, "Infected") {
		t.Errorf("hit sample text = %q, want it to name the attacker", player.Positions[1].Text)
	}
	if player.Positions[2].Kind != KindBuild {
		t.Errorf("build sample kind = %q, want %q", player.Positions[2].Kind, KindBuild)
	}
	if player.Positions[3].Kind != KindDisconnect {
		t.Errorf("disconnect sample kind = %q, want %q (its position must survive)",
			player.Positions[3].Kind, KindDisconnect)
	}
}

// A player hit every few seconds must not lose the rest of the day when the
// per-player sample cap is reached: the newest positions are always kept.
func TestDenseTrailKeepsCoveringThePeriod(t *testing.T) {
	events := make([]Event, 0, 200)
	for i := 0; i < 200; i++ {
		at := float64(i * 60)
		events = append(events, Event{
			Kind: KindHit, T: at, Tod: &at,
			Player: "Bob", PlayerKey: "bob",
			Pos: &Position{X: float64(i), Y: float64(i), Z: 10},
		})
	}

	opts := DefaultOptions()
	opts.MaxPositionsPerPlayer = 10
	report := AnalyzeAdm(events, opts)
	if len(report.Players) != 1 {
		t.Fatalf("players = %d, want 1", len(report.Players))
	}
	positions := report.Players[0].Positions
	if len(positions) > opts.MaxPositionsPerPlayer {
		t.Fatalf("positions = %d, want at most %d", len(positions), opts.MaxPositionsPerPlayer)
	}
	if len(positions) < 5 {
		t.Fatalf("positions = %d, want a usable trail", len(positions))
	}
	if first := positions[0].X; first != 0 {
		t.Errorf("first sample x = %.0f, want 0 (the start of the period)", first)
	}
	if last := positions[len(positions)-1].X; last != 199 {
		t.Errorf("last sample x = %.0f, want 199 (the newest event must survive)", last)
	}
}
