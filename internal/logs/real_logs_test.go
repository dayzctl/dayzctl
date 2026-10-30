package logs

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// realFixture stages the captured production logs used as regression fixtures.
func realFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles", "real")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, name := range []string{"real.ADM", "real.RPT"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(profiles, name), data, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return dir
}

// TestAnalyzeRealDayZLogs pins the behaviour that only real log files revealed:
// kill lines with two parenthetical groups, "meters" instead of "m", hit lines
// carrying an [HP: …] marker, and -3.4e38 sentinel positions.
func TestAnalyzeRealDayZLogs(t *testing.T) {
	svc := NewService(realFixture(t), DefaultOptions())

	report, err := svc.Analyze("real", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if report.RptOrigin != OriginServer {
		t.Errorf("rptOrigin = %s, want server", report.RptOrigin)
	}

	adm := report.Adm
	if adm == nil {
		t.Fatal("expected an ADM report")
	}

	// ── player-versus-player kill ────────────────────────
	// Player "MatenmePorqueMeMuero" (DEAD) (id=… pos=<…>) killed by
	// Player "Autopsy" (id=… pos=<…>) with KA-101 from 10.7674 meters
	if adm.Totals.PvPKills != 1 {
		t.Fatalf("pvp kills = %d, want 1", adm.Totals.PvPKills)
	}
	kill := adm.Kills[0]
	if kill.Killer != "Autopsy" || kill.Victim != "MatenmePorqueMeMuero" {
		t.Errorf("kill = %s -> %s", kill.Killer, kill.Victim)
	}
	if kill.Weapon != "KA-101" {
		t.Errorf("weapon = %q, want KA-101", kill.Weapon)
	}
	if kill.Distance == nil || math.Abs(*kill.Distance-10.7674) > 0.001 {
		t.Errorf("distance = %v, want 10.7674", kill.Distance)
	}
	if kill.Pos == nil {
		t.Error("expected the victim's position on the kill")
	}

	// ── damage taken ─────────────────────────────────────
	// Infected, fall damage and vehicle hits carry no attacker but still count.
	var hitsTaken int
	for _, p := range adm.Players {
		hitsTaken += p.HitsTaken
	}
	if hitsTaken == 0 {
		t.Error("expected damage taken to be counted")
	}

	// ── sentinel positions ───────────────────────────────
	// pos=<-3.4e38, …> must be dropped, not plotted.
	for _, p := range adm.Players {
		for _, sample := range p.Positions {
			if math.Abs(sample.X) > maxCoord || math.Abs(sample.Y) > maxCoord {
				t.Fatalf("player %s has an absurd position sample: %+v", p.Name, sample)
			}
		}
	}

	// ── respawns are not teleports ───────────────────────
	for _, s := range adm.Suspicious {
		if s.Type == "teleport" {
			t.Errorf("respawn/reconnect reported as impossible movement: %s (%s)", s.Player, s.Detail)
		}
	}

	if adm.Totals.Deaths < 3 {
		t.Errorf("deaths = %d, want at least 3 (vehicle, suicide, infected)", adm.Totals.Deaths)
	}

	// ── RPT ──────────────────────────────────────────────
	rpt := report.Rpt
	if rpt == nil {
		t.Fatal("expected an RPT report")
	}

	byRule := map[string]RptGroup{}
	for _, g := range rpt.Groups {
		byRule[g.RuleID] = g
	}

	telemetry, ok := byRule["server-telemetry"]
	if !ok {
		t.Fatal("expected the per-second FPS/memory/players lines to be grouped")
	}
	if telemetry.Count < 100 {
		t.Errorf("telemetry group count = %d, want the whole run grouped together", telemetry.Count)
	}
	if !telemetry.Vanilla {
		t.Error("telemetry should be marked as vanilla noise")
	}

	entity, ok := byRule["entity-warning"]
	if !ok {
		t.Fatal("expected ENTITY (W) lines to be recognised")
	}
	if entity.Severity != SeverityWarning || !entity.Vanilla {
		t.Errorf("entity warning = %s vanilla=%v", entity.Severity, entity.Vanilla)
	}

	// Grouping must keep a million-line file readable.
	if rpt.GroupCount > 2000 {
		t.Errorf("groups = %d, expected far fewer distinct patterns", rpt.GroupCount)
	}
}

func TestRealAdmParsesConnectingLines(t *testing.T) {
	// "is connecting" is a handshake, not a session: only "is connected" joins.
	if got := ParseAdmMessage(`Player "Bob" (id=abc) is connecting`).Kind; got != KindOther {
		t.Errorf("is connecting = %s, want %s", got, KindOther)
	}
	if got := ParseAdmMessage(`Player "Bob" (id=abc pos=<1.0, 2.0, 3.0>) is connected`).Kind; got != KindConnect {
		t.Errorf("is connected = %s, want %s", got, KindConnect)
	}
	if got := ParseAdmMessage(`Player "Bob" (id=abc) has been disconnected`).Kind; got != KindDisconnect {
		t.Errorf("has been disconnected = %s, want %s", got, KindDisconnect)
	}
}

func TestRealAdmKillVariants(t *testing.T) {
	cases := []struct {
		line   string
		weapon string
		dist   float64
	}{
		{
			`Player "A" (DEAD) (id=x pos=<1, 2, 3>) killed by Player "B" (id=y pos=<4, 5, 6>) with KA-101 from 10.7674 meters`,
			"KA-101", 10.7674,
		},
		{
			`Player "A" killed by Player "B" with M4A1 from 41.2 m`,
			"M4A1", 41.2,
		},
		{
			`Player "A" (id=x) killed by Player "B" with AK74`,
			"AK74", 0,
		},
	}

	for _, tc := range cases {
		parsed := ParseAdmMessage(tc.line)
		if parsed.Kind != KindKill {
			t.Errorf("%q: kind = %s", tc.line, parsed.Kind)
			continue
		}
		if parsed.Weapon != tc.weapon {
			t.Errorf("%q: weapon = %q, want %q", tc.line, parsed.Weapon, tc.weapon)
		}
		if tc.dist > 0 && (parsed.Distance == nil || math.Abs(*parsed.Distance-tc.dist) > 0.001) {
			t.Errorf("%q: distance = %v, want %v", tc.line, parsed.Distance, tc.dist)
		}
	}
}

func TestRealAdmHitVariants(t *testing.T) {
	player := ParseAdmMessage(`Player "A" (id=x pos=<1, 2, 3>)[HP: 4.82861] hit by Player "B" (id=y pos=<4, 5, 6>) into Torso(15) for 95.1714 damage (Bullet_556x45) with KA-101 from 13.6119 meters`)
	if player.Kind != KindHit || player.Other != "B" {
		t.Fatalf("player hit = %+v", player)
	}
	if player.Damage == nil || *player.Damage != 95 {
		t.Errorf("damage = %v, want 95 (not the hitbox index 15)", player.Damage)
	}
	if player.Hitbox != "Torso" {
		t.Errorf("hitbox = %q", player.Hitbox)
	}

	infected := ParseAdmMessage(`Player "A" (id=x pos=<1, 2, 3>)[HP: 98.5] hit by Infected into LeftLeg(8) for 5.85 damage (MeleeInfected)`)
	if infected.Kind != KindHit || infected.Other != "" {
		t.Errorf("infected hit = %+v, want a hit with no attacker", infected)
	}
	if infected.Damage == nil || *infected.Damage != 6 {
		t.Errorf("damage = %v, want 6 (rounded from 5.85)", infected.Damage)
	}
}

func TestRealAdmPositions(t *testing.T) {
	parser := &AdmParser{}
	parser.Feed(`02:03:34 | Player "Bodden55" (id=76561198145740672 pos=<-340282346638528859811704183484516925440.0, -340282346638528859811704183484516925440.0, -340282346638528859811704183484516925440.0>) has been disconnected`)
	parser.Feed(`02:04:33 | Player "Bodden55" (id=abc pos=<8626.1, 8633.6, 266.2>) is connected`)

	if len(parser.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(parser.Events))
	}
	if parser.Events[0].Pos != nil {
		t.Errorf("sentinel position should be dropped, got %+v", *parser.Events[0].Pos)
	}
	pos := parser.Events[1].Pos
	if pos == nil || pos.X != 8626.1 || pos.Y != 8633.6 || pos.Z != 266.2 {
		t.Errorf("pos = %+v", pos)
	}
}

func TestRealRptSeverityAndGrouping(t *testing.T) {
	parser := NewRptParser()
	for _, line := range []string{
		` 9:59:59 Players: 1 in total`,
		`10:00:00 ENTITY    (W): dz\structures_bliss\industrial\power\power_vhv_tower_noproxy.p3d: house, config class missing`,
		`10:00:00 Average server FPS: 612.00 (measured interval: 1 s)`,
		`10:00:00 Used memory: 3348068 KB`,
		`10:00:56 [CE][Links] found 294 total links between groups, of 299`,
		`18:20:00 !!! [CE][LoadPrototype] 3 Errors during XML parse`,
	} {
		parser.Feed(line)
	}
	result := parser.Finish()

	counts := result.Counts
	if counts.Warning != 1 {
		t.Errorf("warnings = %d, want 1 (the ENTITY (W) line)", counts.Warning)
	}
	if counts.Critical != 1 {
		t.Errorf("critical = %d, want 1 (XML parse errors)", counts.Critical)
	}
	if counts.Info != 4 {
		t.Errorf("info = %d, want 4", counts.Info)
	}

	byRule := map[string]RptGroup{}
	for _, g := range result.Groups {
		byRule[g.RuleID] = g
	}
	if _, ok := byRule["entity-warning"]; !ok {
		t.Error("ENTITY (W) was not classified")
	}
	if _, ok := byRule["central-economy-trace"]; !ok {
		t.Error("[CE] bookkeeping was not classified")
	}
	if _, ok := byRule["central-economy"]; !ok {
		t.Error("XML parse errors should be critical")
	}
}
