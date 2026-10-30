package logs

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Event is one recognised ADM line.
type Event struct {
	Kind      string    `json:"kind"`
	T         float64   `json:"t"`
	Tod       *float64  `json:"tod"`
	Player    string    `json:"player,omitempty"`
	PlayerKey string    `json:"playerKey,omitempty"`
	Other     string    `json:"other,omitempty"`
	OtherKey  string    `json:"otherKey,omitempty"`
	Weapon    string    `json:"weapon,omitempty"`
	Distance  *float64  `json:"distance,omitempty"`
	Hitbox    string    `json:"hitbox,omitempty"`
	Damage    *int      `json:"damage,omitempty"`
	Marker    bool      `json:"marker,omitempty"`
	Text      string    `json:"text,omitempty"`
	ID        string    `json:"id,omitempty"`
	Pos       *Position `json:"pos,omitempty"`
	Raw       string    `json:"raw,omitempty"`
}

// DayZ writes one or more parenthetical groups between the name and the verb,
// e.g. `Player "Bob" (DEAD) (id=l766… pos=<1, 2, 3>) killed by …`.
const parenGroup = `(?: \([^)]*\))*`

// hpTag is the "[HP: 97.5146]" marker that precedes hit lines.
const hpTag = `(?:\[HP:[^\]]*\])?`

var (
	reLeadingPipe = regexp.MustCompile(`^\s*\|\s*`)

	// num matches plain and exponent forms so float sentinels can be rejected
	// instead of being plotted at -3.4e38.
	num = `(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)`

	rePos       = regexp.MustCompile(`(?i)pos=<\s*` + num + `\s*,\s*` + num + `\s*,\s*` + num + `\s*>`)
	reAtPos     = regexp.MustCompile(`(?i)\bat\s*<\s*` + num + `\s*,\s*` + num + `\s*,\s*` + num + `\s*>`)
	rePosSuffix = regexp.MustCompile(`(?i)[\s,]*\bat\s*<[^>]*>`)

	reID = regexp.MustCompile(`\bid=([0-9A-Za-z+/=_\-.]{4,})`)

	// "with KA-101 from 10.7674 meters" — the unit is "meters", not "m".
	reDistance   = regexp.MustCompile(`(?i)\bfrom\s+([\d.]+)\s*(?:meters?|metres?|m)\b`)
	reWithWeapon = regexp.MustCompile(`(?i)\bwith\s+([^()]+?)\s*(?:\bfrom\s+[\d.]+\s*(?:meters?|metres?|m)\b)?\s*$`)
	reDamage     = regexp.MustCompile(`(?i)\bfor\s+([\d.]+)\s+damage\b`)
	reHitbox     = regexp.MustCompile(`(?i)\binto\s+(\w+)(?:\((-?\d+)\))?`)

	reChat1 = regexp.MustCompile(`(?i)^Player "([^"]+)"(?: \([^)]*\))*\s+said in chat:\s*(.*)$`)
	reChat2 = regexp.MustCompile(`(?i)^Player "([^"]+)"(?: \([^)]*\))*\s+\(chat\):\s*(.*)$`)
	reChat3 = regexp.MustCompile(`(?i)^Player "([^"]+)"(?: \([^)]*\))*\s+said:\s*(.*)$`)
	reChat4 = regexp.MustCompile(`(?i)^\[Chat\]\s*"?([^":]+)"?\s*:\s*(.*)$`)
	reChat5 = regexp.MustCompile(`(?i)^\(Direct\)\s*"([^"]+)":\s*(.*)$`)

	reKicked        = regexp.MustCompile(`(?i)^Player "([^"]+)"[^"]*(?:has been|was)\s+kicked\b(.*)$`)
	reBanned        = regexp.MustCompile(`(?i)^Player "([^"]+)"[^"]*(?:has been|was)\s+banned\b(.*)$`)
	reKill          = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+killed by Player "([^"]+)"(.*)$`)
	reKillAny       = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+killed by (.+)$`)
	reSuicide       = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+(?:committed suicide|bled out|\bdied\b)`)
	reHit           = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + hpTag + `\s+hit by Player "([^"]+)"(.*)$`)
	reHitAny        = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + hpTag + `\s+hit by (.+)$`)
	reBuilt         = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+built\s+(.+)$`)
	rePlaced        = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+(?:placed|deployed|constructed|packed)\s+(.+)$`)
	reConnect       = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+(?:is\s+)?connected\b(.*)$`)
	reDisc          = regexp.MustCompile(`(?i)^Player "([^"]+)"` + parenGroup + `\s+(?:has been\s+|has\s+)?disconnected\b(.*)$`)
	reAdmin         = regexp.MustCompile(`(?i)^(?:Admin|\[Admin\])\s+([^:]+):\s*(.*)$`)
	reDead          = regexp.MustCompile(`(?i)^Player "([^"]+)"\s+\(DEAD\)`)
	rePlayer        = regexp.MustCompile(`(?i)^Player "([^"]+)"`)
	reAdminLogStart = regexp.MustCompile(`(?i)AdminLog\s+started\s+on\s+(\d{4}-\d{2}-\d{2})`)
)

// PlayerKey normalises a player name for merging ("Bob" == "bob").
func PlayerKey(name string) string {
	n := strings.TrimSpace(name)
	n = strings.Trim(n, `"'`)
	return strings.ToLower(n)
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

// positionBounds keep sentinel values (-3.4e38) and absurd coordinates out of
// the analysis. DayZ maps are at most ~16 km; allow a generous margin.
const (
	maxCoord    = 40000
	maxAltitude = 3000
)

func plausiblePosition(p *Position) bool {
	if p == nil {
		return false
	}
	if p.X < -200 || p.X > maxCoord {
		return false
	}
	if p.Y < -200 || p.Y > maxCoord {
		return false
	}
	if p.Z < -500 || p.Z > maxAltitude {
		return false
	}
	return true
}

func positionFromGroups(m []string) *Position {
	if m == nil {
		return nil
	}
	x, errX := strconv.ParseFloat(m[1], 64)
	y, errY := strconv.ParseFloat(m[2], 64)
	z, errZ := strconv.ParseFloat(m[3], 64)
	if errX != nil || errY != nil || errZ != nil {
		return nil
	}
	pos := &Position{X: x, Y: y, Z: z}
	if !plausiblePosition(pos) {
		return nil
	}
	return pos
}

// parsePosition finds a position in a message ("pos=<x, y, z>" or "at <x, y, z>").
func parsePosition(message string) *Position {
	if pos := positionFromGroups(rePos.FindStringSubmatch(message)); pos != nil {
		return pos
	}
	return positionFromGroups(reAtPos.FindStringSubmatch(message))
}

// weaponTail extracts the weapon and range from
// "… for 95.17 damage (Bullet_556x45) with KA-101 from 13.61 meters".
func weaponTail(tail string) (weapon string, distance *float64) {
	if tail == "" {
		return "", nil
	}
	if m := reDistance.FindStringSubmatch(tail); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			distance = floatPtr(v)
		}
	}
	if m := reWithWeapon.FindStringSubmatch(tail); m != nil {
		if w := strings.TrimSpace(strings.TrimRight(m[1], ".,; ")); w != "" {
			if len(w) > 80 {
				w = w[:80]
			}
			weapon = w
		}
	}
	return weapon, distance
}

func hitDamage(tail string, hitbox []string) *int {
	if m := reDamage.FindStringSubmatch(tail); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			return intPtr(int(v + 0.5))
		}
	}
	// Older logs only carry the number in the hitbox parentheses.
	if len(hitbox) > 2 && hitbox[2] != "" {
		if v, err := strconv.Atoi(hitbox[2]); err == nil {
			return intPtr(v)
		}
	}
	return nil
}

func stripPositionSuffix(text string) string {
	cleaned := strings.TrimSpace(rePosSuffix.ReplaceAllString(text, ""))
	cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, "with Hands"))
	cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, "with hands"))
	return cleaned
}

type admMessage struct {
	Kind     string
	Player   string
	Other    string
	Weapon   string
	Distance *float64
	Hitbox   string
	Damage   *int
	Text     string
	Marker   bool
}

// ParseAdmMessage classifies the message part of an ADM line (timestamp and
// leading "|" already removed).
func ParseAdmMessage(message string) admMessage {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return admMessage{Kind: KindOther}
	}

	// Chat first: its forms also start with `Player "…"`.
	for _, re := range []*regexp.Regexp{reChat1, reChat2, reChat3, reChat4, reChat5} {
		if m := re.FindStringSubmatch(msg); m != nil {
			return admMessage{Kind: KindChat, Player: m[1], Text: m[2]}
		}
	}

	if m := reKicked.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindKick, Player: m[1], Text: strings.TrimSpace(m[2])}
	}
	if m := reBanned.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindBan, Player: m[1], Text: strings.TrimSpace(m[2])}
	}

	if m := reKill.FindStringSubmatch(msg); m != nil {
		weapon, dist := weaponTail(m[3])
		return admMessage{Kind: KindKill, Player: m[1], Other: m[2], Weapon: weapon, Distance: dist}
	}
	if m := reKillAny.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindDeath, Player: m[1], Text: strings.TrimSpace(m[2])}
	}
	if m := reSuicide.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindDeath, Player: m[1], Text: "self"}
	}

	if m := reHit.FindStringSubmatch(msg); m != nil {
		weapon, dist := weaponTail(m[3])
		hitbox := reHitbox.FindStringSubmatch(m[3])
		var hitboxName string
		if hitbox != nil {
			hitboxName = hitbox[1]
		}
		return admMessage{
			Kind: KindHit, Player: m[1], Other: m[2],
			Weapon: weapon, Distance: dist, Hitbox: hitboxName, Damage: hitDamage(m[3], hitbox),
		}
	}
	// Damage without a player attacker: infected, fall damage, vehicles.
	if m := reHitAny.FindStringSubmatch(msg); m != nil {
		hitbox := reHitbox.FindStringSubmatch(m[2])
		var hitboxName string
		if hitbox != nil {
			hitboxName = hitbox[1]
		}
		return admMessage{
			Kind: KindHit, Player: m[1],
			Hitbox: hitboxName, Damage: hitDamage(m[2], hitbox),
		}
	}

	if m := reBuilt.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindBuild, Player: m[1], Text: stripPositionSuffix(m[2])}
	}
	if m := rePlaced.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindPlace, Player: m[1], Text: stripPositionSuffix(m[2])}
	}

	if m := reConnect.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindConnect, Player: m[1], Text: strings.TrimSpace(m[2])}
	}
	if m := reDisc.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindDisconnect, Player: m[1], Text: strings.TrimSpace(m[2])}
	}

	if m := reAdmin.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindAdmin, Player: strings.TrimSpace(m[1]), Text: m[2]}
	}

	if m := reDead.FindStringSubmatch(msg); m != nil {
		return admMessage{Kind: KindDeath, Player: m[1], Marker: true}
	}

	if rePos.MatchString(msg) {
		if m := rePlayer.FindStringSubmatch(msg); m != nil {
			return admMessage{Kind: KindPosition, Player: m[1]}
		}
	}

	return admMessage{Kind: KindOther}
}

// AdmParser accumulates events from one or more ADM files.
type AdmParser struct {
	clock     Clock
	Events    []Event
	LineCount int
	Unmatched int
	HasDate   bool

	minTod, maxTod float64
	hasTod         bool
	lastT          float64
}

func (p *AdmParser) noteTod(tod float64) {
	if !p.hasTod {
		p.minTod, p.maxTod = tod, tod
		p.hasTod = true
		return
	}
	if tod < p.minTod {
		p.minTod = tod
	}
	if tod > p.maxTod {
		p.maxTod = tod
	}
}

// Feed parses one line.
func (p *AdmParser) Feed(line string) {
	p.LineCount++
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	// Banner and file header lines carry no events.
	if strings.HasPrefix(trimmed, "****") {
		return
	}
	// "AdminLog started on 2026-10-26 at 01:20:56" anchors every timestamp in
	// this file to a real day, which is what makes multi-day ADM files
	// comparable instead of colliding on time of day.
	if m := reAdminLogStart.FindStringSubmatch(trimmed); m != nil {
		if day, err := time.Parse("2006-01-02", m[1]); err == nil {
			p.clock.SetBase(day)
		}
		return
	}

	var tod *float64
	t := p.lastT
	message := line

	if ts, ok := ParseTimestamp(line); ok {
		tod = floatPtr(ts.Secs)
		if ts.Day != nil {
			p.HasDate = true
		}
		t = p.clock.Stamp(ts.Secs, ts.Day)
		message = ts.Rest
		p.noteTod(ts.Secs)
	}
	p.lastT = t

	message = strings.TrimSpace(reLeadingPipe.ReplaceAllString(message, ""))
	if message == "" {
		return
	}

	parsed := ParseAdmMessage(message)
	if parsed.Kind == KindOther {
		p.Unmatched++
		return
	}

	event := Event{
		Kind:     parsed.Kind,
		T:        t,
		Tod:      tod,
		Player:   parsed.Player,
		Other:    parsed.Other,
		Weapon:   parsed.Weapon,
		Distance: parsed.Distance,
		Hitbox:   parsed.Hitbox,
		Damage:   parsed.Damage,
		Marker:   parsed.Marker,
		Text:     parsed.Text,
		Pos:      parsePosition(message),
	}
	if parsed.Player != "" {
		event.PlayerKey = PlayerKey(parsed.Player)
	}
	if parsed.Other != "" {
		event.OtherKey = PlayerKey(parsed.Other)
	}
	if m := reID.FindStringSubmatch(message); m != nil {
		event.ID = m[1]
	}
	if len(event.Text) > 300 {
		event.Text = event.Text[:300]
	}
	if len(line) > 400 {
		event.Raw = line[:397] + "…"
	} else {
		event.Raw = line
	}

	p.Events = append(p.Events, event)
}

// SetBaseDate anchors all timestamps of this parser to a calendar day. The
// service calls it per file, using the date in the file name or header.
func (p *AdmParser) SetBaseDate(day time.Time) {
	p.clock.SetBase(day)
	p.HasDate = true
}

// Tods returns the observed time-of-day range.
func (p *AdmParser) Tods() (min, max float64, ok bool) {
	return p.minTod, p.maxTod, p.hasTod
}
