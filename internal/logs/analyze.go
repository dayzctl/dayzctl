package logs

import (
	"regexp"
	"sort"
	"time"
)

// steamID64 matches a real 64-bit Steam account id. The `id=` field in a DayZ
// ADM is a per-session token in current builds, so only SteamIDs are stable
// enough to compare across sessions.
var steamID64 = regexp.MustCompile(`^7656119\d{10}$`)

// Options tunes the heuristics that flag suspicious activity.
type Options struct {
	// CombatLogWindow is how soon after combat a disconnect is reported as
	// combat logging (seconds).
	CombatLogWindow float64
	// TeleportSpeed is the implied speed (m/s) above which movement between
	// two samples is not humanly possible. 100 m/s (360 km/h) leaves room for
	// helicopters, which legitimately reach ~250 km/h.
	TeleportSpeed float64
	// TeleportMinDistance is the minimum jump (m) worth checking.
	TeleportMinDistance float64
	// ShortSession flags sessions shorter than this (seconds).
	ShortSession float64
	// MaxPositionsPerPlayer bounds memory on very long logs.
	MaxPositionsPerPlayer int
}

// DefaultOptions returns the documented defaults.
func DefaultOptions() Options {
	return Options{
		CombatLogWindow:       120,
		TeleportSpeed:         100,
		TeleportMinDistance:   150,
		ShortSession:          60,
		MaxPositionsPerPlayer: 1500,
	}
}

// Point2D is a map-plane position (x east, y north).
type Point2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Session is one connect→disconnect span.
type Session struct {
	Start  *float64 `json:"start"`
	End    *float64 `json:"end"`
	Deaths int      `json:"deaths"`
	Kills  int      `json:"kills"`
}

// PosSample is a recorded position at a point in time.
type PosSample struct {
	At *float64 `json:"at"`
	X  float64  `json:"x"`
	Y  float64  `json:"y"`
	Z  float64  `json:"z"`
}

// WeaponUse is a kill count for one weapon.
type WeaponUse struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Player is one aggregated player.
type Player struct {
	Key           string      `json:"key"`
	Name          string      `json:"name"`
	Aliases       []string    `json:"aliases"`
	IDs           []string    `json:"ids"`
	Connects      int         `json:"connects"`
	Disconnects   int         `json:"disconnects"`
	Kills         int         `json:"kills"`
	Deaths        int         `json:"deaths"`
	PvPDeaths     int         `json:"pvpDeaths"`
	AiDeaths      int         `json:"aiDeaths"`
	Chats         int         `json:"chats"`
	Builds        int         `json:"builds"`
	HitsDealt     int         `json:"hitsDealt"`
	HitsTaken     int         `json:"hitsTaken"`
	DamageDealt   int         `json:"damageDealt"`
	Playtime      float64     `json:"playtime"`
	Sessions      []Session   `json:"sessions"`
	Positions     []PosSample `json:"positions"`
	Weapons       []WeaponUse `json:"weapons"`
	FirstAt       *float64    `json:"firstAt"`
	LastAt        *float64    `json:"lastAt"`
	LastKnownPos  *Position   `json:"lastKnownPos"`
	PositionCount int         `json:"positionCount"`
	KD            float64     `json:"kd"`
	Suspicion     int         `json:"suspicion"`
}

// Kill is one player-versus-player kill.
type Kill struct {
	At        *float64  `json:"at"`
	Victim    string    `json:"victim"`
	VictimKey string    `json:"victimKey"`
	Killer    string    `json:"killer"`
	KillerKey string    `json:"killerKey"`
	Weapon    string    `json:"weapon"`
	Distance  *float64  `json:"distance"`
	Pos       *Position `json:"pos"`
	Raw       string    `json:"raw"`
}

// Death is a non-PvP or marker death.
type Death struct {
	At        *float64  `json:"at"`
	Player    string    `json:"player"`
	PlayerKey string    `json:"playerKey"`
	Cause     string    `json:"cause"`
	Pos       *Position `json:"pos"`
}

// Chat is one chat line.
type Chat struct {
	At        *float64 `json:"at"`
	Player    string   `json:"player"`
	PlayerKey string   `json:"playerKey"`
	Text      string   `json:"text"`
}

// Build is a build/place action.
type Build struct {
	At        *float64  `json:"at"`
	Player    string    `json:"player"`
	PlayerKey string    `json:"playerKey"`
	Object    string    `json:"object"`
	Pos       *Position `json:"pos"`
	Kind      string    `json:"kind"`
}

// Suspicion is one flagged event.
type Suspicion struct {
	Type      string   `json:"type"`
	At        *float64 `json:"at"`
	Player    string   `json:"player"`
	PlayerKey string   `json:"playerKey"`
	Detail    string   `json:"detail"`
	Seconds   *float64 `json:"seconds,omitempty"`
	From      *Point2D `json:"from,omitempty"`
	To        *Point2D `json:"to,omitempty"`
	Speed     *float64 `json:"speed,omitempty"`
}

// Period is the observed time span.
type Period struct {
	HasTime       bool     `json:"hasTime"`
	From          *float64 `json:"from"`
	To            *float64 `json:"to"`
	Seconds       float64  `json:"seconds"`
	SpansMidnight bool     `json:"spansMidnight"`
	HasDate       bool     `json:"hasDate"`
}

// Bounds is the extent of all recorded positions. It lets the map scale itself
// for custom maps whose world size we do not know in advance.
type Bounds struct {
	MinX float64 `json:"minX"`
	MaxX float64 `json:"maxX"`
	MinY float64 `json:"minY"`
	MaxY float64 `json:"maxY"`
}

// Totals are the overview counters.
type Totals struct {
	Players    int `json:"players"`
	Connects   int `json:"connects"`
	Kills      int `json:"kills"`
	PvPKills   int `json:"pvpKills"`
	AiDeaths   int `json:"aiDeaths"`
	Deaths     int `json:"deaths"`
	Chats      int `json:"chats"`
	Builds     int `json:"builds"`
	Suspicious int `json:"suspicious"`
}

// AdmReport is the aggregated admin-log analysis.
type AdmReport struct {
	Events     int         `json:"events"`
	Players    []Player    `json:"players"`
	Kills      []Kill      `json:"kills"`
	Deaths     []Death     `json:"deaths"`
	Chats      []Chat      `json:"chats"`
	Builds     []Build     `json:"builds"`
	Suspicious []Suspicion `json:"suspicious"`
	Period     Period      `json:"period"`
	PeakOnline int         `json:"peakOnline"`
	PeakTod    *float64    `json:"peakTod"`
	Totals     Totals      `json:"totals"`
	Bounds     *Bounds     `json:"bounds,omitempty"`
}

type playerState struct {
	key          string
	name         string
	aliases      map[string]struct{}
	ids          map[string]struct{}
	connects     int
	disconnects  int
	kills        int
	deaths       int
	pvpDeaths    int
	aiDeaths     int
	chats        int
	builds       int
	hitsDealt    int
	hitsTaken    int
	damageDealt  int
	playtime     float64
	sessions     []Session
	positions    []PosSample
	weapons      map[string]int
	firstAt      *float64
	lastAt       *float64
	lastKnown    *Position
	currentIdx   int
	lastHitAt    *float64
	lastKillAt   *float64
	lastDeathAt  *float64
	pendingDeath *float64
	suspicion    int
	// breaks mark positions that must not be compared with the previous
	// sample: respawns and reconnects legitimately relocate a player.
	breaks []float64
}

func newPlayerState(key, name string) *playerState {
	return &playerState{
		key:        key,
		name:       name,
		aliases:    map[string]struct{}{name: {}},
		ids:        map[string]struct{}{},
		weapons:    map[string]int{},
		currentIdx: -1,
	}
}

func (p *playerState) touch(at *float64) {
	if at == nil {
		return
	}
	if p.firstAt == nil || *at < *p.firstAt {
		p.firstAt = at
	}
	if p.lastAt == nil || *at > *p.lastAt {
		p.lastAt = at
	}
}

func (p *playerState) pushPosition(at *float64, pos *Position, max int) {
	if pos == nil {
		return
	}
	p.lastKnown = pos
	if len(p.positions) < max {
		p.positions = append(p.positions, PosSample{At: at, X: pos.X, Y: pos.Y, Z: pos.Z})
	}
}

func (p *playerState) session() *Session {
	if p.currentIdx < 0 || p.currentIdx >= len(p.sessions) {
		return nil
	}
	return &p.sessions[p.currentIdx]
}

// addBreak records a discontinuity at `at` (death or reconnect).
func (p *playerState) addBreak(at *float64) {
	if at != nil {
		p.breaks = append(p.breaks, *at)
	}
}

// hasBreakBetween reports whether a discontinuity happened in (from, to].
func hasBreakBetween(breaks []float64, from, to float64) bool {
	for _, b := range breaks {
		if b > from && b <= to {
			return true
		}
	}
	return false
}

// AnalyzeAdm aggregates ADM events into the report the UI renders.
func AnalyzeAdm(events []Event, opts Options) *AdmReport {
	// Stable order by time of day; untimed events keep their file order at the end.
	ordered := make([]int, len(events))
	for i := range ordered {
		ordered[i] = i
	}
	// Order by absolute time. Events carry a day-anchored timestamp, so files
	// from different days interleave correctly instead of by time of day.
	sort.SliceStable(ordered, func(a, b int) bool {
		return events[ordered[a]].T < events[ordered[b]].T
	})

	players := make(map[string]*playerState)
	var kills []Kill
	var deaths []Death
	var chats []Chat
	var builds []Build
	var suspicious []Suspicion
	idOwners := make(map[string]string)

	var minT, maxT float64
	hasTime := false
	online, peakOnline := 0, 0
	var peakTod *float64
	pvpKills, aiDeaths := 0, 0

	getPlayer := func(key, name string) *playerState {
		p, ok := players[key]
		if !ok {
			p = newPlayerState(key, name)
			players[key] = p
			return p
		}
		if name != "" {
			p.aliases[name] = struct{}{}
		}
		return p
	}

	noteTime := func(t float64) {
		if !hasTime {
			minT, maxT = t, t
			hasTime = true
			return
		}
		if t < minT {
			minT = t
		}
		if t > maxT {
			maxT = t
		}
	}

	for _, idx := range ordered {
		e := events[idx]
		// `Tod` is only present when the line had a timestamp; `T` is always
		// usable (continuation lines inherit the previous line's time).
		if e.Tod != nil {
			noteTime(e.T)
		}
		at := &e.T

		if e.ID != "" && e.PlayerKey != "" {
			// Only a real SteamID identifies a person across sessions; the
			// `id=` token is expected to change on every join.
			if steamID64.MatchString(e.ID) {
				if owner, ok := idOwners[e.ID]; ok && owner != e.PlayerKey {
					p := getPlayer(e.PlayerKey, e.Player)
					p.suspicion++
					ownerName := owner
					if o, ok := players[owner]; ok {
						ownerName = o.name
					}
					suspicious = append(suspicious, Suspicion{
						Type:      "shared-id",
						At:        at,
						Player:    e.Player,
						PlayerKey: e.PlayerKey,
						Detail:    "The same SteamID " + e.ID + ` is also used by "` + ownerName + `"`,
					})
				} else {
					idOwners[e.ID] = e.PlayerKey
				}
			}
			getPlayer(e.PlayerKey, e.Player).ids[e.ID] = struct{}{}
		}

		switch e.Kind {
		case KindConnect:
			if e.PlayerKey == "" {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			p.touch(at)
			if e.Pos != nil {
				p.pushPosition(at, e.Pos, opts.MaxPositionsPerPlayer)
			}

			// DayZ writes several "is connected" lines per join (Steam
			// handshake, then the spawn line carrying the position). Only the
			// first opens a session or counts and playtime inflate.
			if s := p.session(); s != nil {
				if s.Start != nil && at != nil && *at-*s.Start <= 120 {
					// …unless the player died in between: that "connected" line is
					// a respawn and starts a new session.
					if !hasBreakBetween(p.breaks, *s.Start, *at) {
						continue
					}
				}
				// Long after the session started, a second connect means the
				// disconnect line was missing (log rotation): close and reopen.
				if s.End == nil {
					s.End = at
				}
			} else {
				online++
				if online > peakOnline {
					peakOnline = online
					peakTod = at
				}
			}

			p.connects++
			p.sessions = append(p.sessions, Session{Start: at})
			p.currentIdx = len(p.sessions) - 1
			p.addBreak(at)

		case KindDisconnect:
			if e.PlayerKey == "" {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			p.disconnects++
			p.touch(at)
			if s := p.session(); s != nil && s.End == nil {
				s.End = at
				p.currentIdx = -1
			}
			if online > 0 {
				online--
			}

			checks := []struct {
				reason string
				ref    *float64
			}{
				{"after being hit", p.lastHitAt},
				{"after killing a player", p.lastKillAt},
				{"after dying", p.lastDeathAt},
			}
			for _, c := range checks {
				if c.ref == nil {
					continue
				}
				ref := *c.ref
				now := ref
				if at != nil {
					now = *at
				}
				delta := now - ref
				if delta >= 0 && delta <= opts.CombatLogWindow {
					p.suspicion += 2
					secs := delta
					suspicious = append(suspicious, Suspicion{
						Type:      "combat-log",
						At:        at,
						Player:    p.name,
						PlayerKey: p.key,
						Detail:    "Disconnected " + formatSeconds(delta) + "s " + c.reason,
						Seconds:   &secs,
					})
					break
				}
			}

		case KindKill:
			if e.PlayerKey == "" || e.OtherKey == "" {
				continue
			}
			victim := getPlayer(e.PlayerKey, e.Player)
			killer := getPlayer(e.OtherKey, e.Other)

			alreadyCounted := victim.pendingDeath != nil && at != nil && absFloat(*at-*victim.pendingDeath) <= 5
			if !alreadyCounted {
				victim.deaths++
			}
			victim.pvpDeaths++

			// The kill line itself has no coordinates; the death marker does.
			var markerPos *Position
			if victim.pendingDeath != nil && at != nil && absFloat(*at-*victim.pendingDeath) <= 10 {
				markerPos = victim.lastKnown
			}
			killPos := e.Pos
			if killPos == nil {
				killPos = markerPos
			}

			victim.pendingDeath = nil
			victim.lastDeathAt = at
			victim.addBreak(at)
			if s := victim.session(); s != nil {
				s.Deaths++
			}
			if killPos != nil {
				victim.pushPosition(at, killPos, opts.MaxPositionsPerPlayer)
			}

			killer.kills++
			killer.lastKillAt = at
			if s := killer.session(); s != nil {
				s.Kills++
			}
			weapon := e.Weapon
			if weapon == "" {
				weapon = "unknown"
			}
			killer.weapons[weapon]++

			pvpKills++
			victim.touch(at)
			killer.touch(at)

			kills = append(kills, Kill{
				At:        at,
				Victim:    victim.name,
				VictimKey: victim.key,
				Killer:    killer.name,
				KillerKey: killer.key,
				Weapon:    e.Weapon,
				Distance:  e.Distance,
				Pos:       killPos,
				Raw:       e.Raw,
			})

		case KindDeath:
			if e.PlayerKey == "" {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			p.touch(at)
			if e.Pos != nil {
				p.pushPosition(at, e.Pos, opts.MaxPositionsPerPlayer)
			}

			// A bare "(DEAD)" line is only a marker: the cause line follows.
			if e.Marker {
				p.pendingDeath = at
				continue
			}

			p.pendingDeath = nil
			p.deaths++
			p.aiDeaths++
			p.lastDeathAt = at
			p.addBreak(at)
			if s := p.session(); s != nil {
				s.Deaths++
			}
			aiDeaths++
			deaths = append(deaths, Death{
				At:        at,
				Player:    p.name,
				PlayerKey: p.key,
				Cause:     e.Text,
				Pos:       e.Pos,
			})

		case KindHit:
			if e.PlayerKey == "" {
				continue
			}
			victim := getPlayer(e.PlayerKey, e.Player)
			victim.hitsTaken++
			victim.touch(at)
			if e.Pos != nil {
				victim.pushPosition(at, e.Pos, opts.MaxPositionsPerPlayer)
			}
			if e.OtherKey == "" {
				// Environmental damage (infected, falls, vehicles) is not combat,
				// so it must not feed the combat-log heuristic.
				break
			}
			shooter := getPlayer(e.OtherKey, e.Other)
			victim.lastHitAt = at
			shooter.hitsDealt++
			if e.Damage != nil {
				shooter.damageDealt += *e.Damage
			}
			shooter.touch(at)

		case KindChat:
			if e.PlayerKey == "" {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			p.chats++
			p.touch(at)
			chats = append(chats, Chat{At: at, Player: p.name, PlayerKey: p.key, Text: e.Text})

		case KindBuild, KindPlace:
			if e.PlayerKey == "" {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			p.builds++
			p.touch(at)
			builds = append(builds, Build{
				At: at, Player: p.name, PlayerKey: p.key,
				Object: e.Text, Pos: e.Pos, Kind: e.Kind,
			})
			if e.Pos != nil {
				p.lastKnown = e.Pos
			}

		case KindPosition:
			if e.PlayerKey == "" || e.Pos == nil {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			p.pushPosition(at, e.Pos, opts.MaxPositionsPerPlayer)
			p.touch(at)

		case KindKick, KindBan:
			if e.PlayerKey == "" {
				continue
			}
			p := getPlayer(e.PlayerKey, e.Player)
			detail := e.Text
			if detail == "" {
				if e.Kind == KindBan {
					detail = "Banned"
				} else {
					detail = "Kicked"
				}
			}
			suspicious = append(suspicious, Suspicion{
				Type: e.Kind, At: at, Player: p.name, PlayerKey: p.key, Detail: detail,
			})
			p.suspicion++
		}
	}

	spansMidnight := false
	periodSeconds := 0.0
	if hasTime && maxT > minT {
		periodSeconds = maxT - minT
	}

	// A (DEAD) marker with no cause line still means a death happened.
	for _, p := range players {
		if p.pendingDeath == nil {
			continue
		}
		covered := p.lastDeathAt != nil && absFloat(*p.pendingDeath-*p.lastDeathAt) <= 5
		if !covered {
			p.deaths++
			p.lastDeathAt = p.pendingDeath
		}
		p.pendingDeath = nil
	}

	// Playtime and drive-by sessions.
	for _, p := range players {
		total := 0.0
		for i := range p.sessions {
			s := &p.sessions[i]
			if s.Start == nil {
				continue
			}
			end := s.Start
			if s.End != nil {
				end = s.End
			} else if hasTime {
				end = &maxT
			}
			dur := *end - *s.Start
			if dur > 0 {
				total += dur
			}
			if s.End != nil && *s.End-*s.Start < opts.ShortSession {
				dur := *s.End - *s.Start
				suspicious = append(suspicious, Suspicion{
					Type: "short-session", At: s.Start, Player: p.name, PlayerKey: p.key,
					Detail: "Session of only " + formatSeconds(dur) + "s", Seconds: &dur,
				})
				p.suspicion++
			}
		}
		p.playtime = total
	}

	// Impossible movement between consecutive samples.
	for _, p := range players {
		samples := make([]PosSample, 0, len(p.positions))
		for _, s := range p.positions {
			if s.At != nil {
				samples = append(samples, s)
			}
		}
		sort.SliceStable(samples, func(a, b int) bool { return *samples[a].At < *samples[b].At })

		for i := 1; i < len(samples); i++ {
			a, b := samples[i-1], samples[i]
			dt := *b.At - *a.At
			if dt < 1 {
				continue
			}
			// A respawn or reconnect between two samples relocates the player
			// legitimately; only uninterrupted movement can be impossible.
			if hasBreakBetween(p.breaks, *a.At, *b.At) {
				continue
			}
			dist := hypot(b.X-a.X, b.Y-a.Y)
			if dist < opts.TeleportMinDistance {
				continue
			}
			speed := dist / dt
			if speed > opts.TeleportSpeed {
				p.suspicion += 3
				sp := speed
				suspicious = append(suspicious, Suspicion{
					Type: "teleport", At: b.At, Player: p.name, PlayerKey: p.key,
					Detail: formatMeters(dist) + " m in " + formatSeconds(dt) + "s (" + formatMeters(speed) + " m/s)",
					From:   &Point2D{X: a.X, Y: a.Y},
					To:     &Point2D{X: b.X, Y: b.Y},
					Speed:  &sp,
				})
			}
		}
	}

	sort.SliceStable(suspicious, func(i, j int) bool {
		a, b := suspicious[i].At, suspicious[j].At
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return *a < *b
	})

	list := make([]Player, 0, len(players))
	totals := Totals{}
	for _, p := range players {
		player := Player{
			Key:           p.key,
			Name:          p.name,
			Aliases:       sortedKeys(p.aliases),
			IDs:           sortedKeys(p.ids),
			Connects:      p.connects,
			Disconnects:   p.disconnects,
			Kills:         p.kills,
			Deaths:        p.deaths,
			PvPDeaths:     p.pvpDeaths,
			AiDeaths:      p.aiDeaths,
			Chats:         p.chats,
			Builds:        p.builds,
			HitsDealt:     p.hitsDealt,
			HitsTaken:     p.hitsTaken,
			DamageDealt:   p.damageDealt,
			Playtime:      p.playtime,
			Sessions:      p.sessions,
			Positions:     p.positions,
			Weapons:       weaponUses(p.weapons),
			FirstAt:       p.firstAt,
			LastAt:        p.lastAt,
			LastKnownPos:  p.lastKnown,
			PositionCount: len(p.positions),
			Suspicion:     p.suspicion,
		}
		if p.deaths == 0 {
			player.KD = float64(p.kills)
		} else {
			player.KD = float64(p.kills) / float64(p.deaths)
		}
		if player.Sessions == nil {
			player.Sessions = []Session{}
		}
		if player.Positions == nil {
			player.Positions = []PosSample{}
		}
		list = append(list, player)
		totals.Connects += p.connects
	}

	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Playtime != list[j].Playtime {
			return list[i].Playtime > list[j].Playtime
		}
		return list[i].Kills > list[j].Kills
	})

	var bounds *Bounds
	for i := range list {
		for _, sample := range list[i].Positions {
			if bounds == nil {
				bounds = &Bounds{MinX: sample.X, MaxX: sample.X, MinY: sample.Y, MaxY: sample.Y}
				continue
			}
			if sample.X < bounds.MinX {
				bounds.MinX = sample.X
			}
			if sample.X > bounds.MaxX {
				bounds.MaxX = sample.X
			}
			if sample.Y < bounds.MinY {
				bounds.MinY = sample.Y
			}
			if sample.Y > bounds.MaxY {
				bounds.MaxY = sample.Y
			}
		}
	}

	totals.Players = len(list)
	totals.Kills = len(kills)
	totals.PvPKills = pvpKills
	totals.AiDeaths = aiDeaths
	totals.Deaths = len(kills) + aiDeaths
	totals.Chats = len(chats)
	totals.Builds = len(builds)
	totals.Suspicious = len(suspicious)

	return &AdmReport{
		Events:     len(events),
		Players:    list,
		Kills:      nonNilKills(kills),
		Deaths:     nonNilDeaths(deaths),
		Chats:      nonNilChats(chats),
		Builds:     nonNilBuilds(builds),
		Suspicious: nonNilSuspicion(suspicious),
		Period: Period{
			HasTime:       hasTime,
			From:          optionalFloat(hasTime, minT),
			To:            optionalFloat(hasTime, maxT),
			Seconds:       periodSeconds,
			SpansMidnight: spansMidnight,
		},
		PeakOnline: peakOnline,
		PeakTod:    peakTod,
		Totals:     totals,
		Bounds:     bounds,
	}
}

func optionalFloat(ok bool, v float64) *float64 {
	if !ok {
		return nil
	}
	return floatPtr(v)
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func weaponUses(m map[string]int) []WeaponUse {
	out := make([]WeaponUse, 0, len(m))
	for name, count := range m {
		out = append(out, WeaponUse{Name: name, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func nonNilKills(v []Kill) []Kill {
	if v == nil {
		return []Kill{}
	}
	return v
}

func nonNilDeaths(v []Death) []Death {
	if v == nil {
		return []Death{}
	}
	return v
}

func nonNilChats(v []Chat) []Chat {
	if v == nil {
		return []Chat{}
	}
	return v
}

func nonNilBuilds(v []Build) []Build {
	if v == nil {
		return []Build{}
	}
	return v
}

func nonNilSuspicion(v []Suspicion) []Suspicion {
	if v == nil {
		return []Suspicion{}
	}
	return v
}

// Report is the full analysis payload served by the API.
type Report struct {
	Instance    string      `json:"instance,omitempty"`
	Files       []FileInfo  `json:"files"`
	Adm         *AdmReport  `json:"adm"`
	Rpt         *RptResult  `json:"rpt"`
	RptOrigin   RPTOrigin   `json:"rptOrigin"`
	Stats       ReportStats `json:"stats"`
	GeneratedAt time.Time   `json:"generatedAt"`
	FromCache   bool        `json:"fromCache"`
}

// FileInfo describes one analysed file.
type FileInfo struct {
	Name  string   `json:"name"`
	Type  FileType `json:"type"`
	Lines int      `json:"lines"`
	Bytes int64    `json:"bytes"`
	Error string   `json:"error,omitempty"`
}

// ReportStats are diagnostics about the run.
type ReportStats struct {
	AdmFiles     int `json:"admFiles"`
	RptFiles     int `json:"rptFiles"`
	AdmEvents    int `json:"admEvents"`
	AdmUnmatched int `json:"admUnmatched"`
}
