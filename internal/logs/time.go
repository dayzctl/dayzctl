package logs

import (
	"regexp"
	"strconv"
	"time"
)

// Severity ranks an engine message by how much an admin should care.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityError    Severity = "error"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// SeverityOrder is the display order used by the UI.
var SeverityOrder = []Severity{SeverityCritical, SeverityError, SeverityWarning, SeverityInfo}

// FileType identifies which parser a log file needs.
type FileType string

const (
	FileADM     FileType = "ADM"
	FileRPT     FileType = "RPT"
	FileScript  FileType = "SCRIPT"
	FileCrash   FileType = "CRASH"
	FileUnknown FileType = "UNKNOWN"
)

// ADM event kinds.
const (
	KindConnect    = "connect"
	KindDisconnect = "disconnect"
	KindKill       = "kill"
	KindHit        = "hit"
	KindDeath      = "death"
	KindChat       = "chat"
	KindBuild      = "build"
	KindPlace      = "place"
	KindKick       = "kick"
	KindBan        = "ban"
	KindPosition   = "position"
	KindAdmin      = "admin"
	KindOther      = "other"
)

// Position is a DayZ world position from pos=<x, y, z>:
// x = east, y = north, z = altitude (metres).
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Timestamp is the parsed prefix of a log line.
type Timestamp struct {
	// Secs is the time of day in seconds (0..86399).
	Secs float64
	// Day is set for lines that carry a calendar date.
	Day *time.Time
	// Rest is the remainder of the line after the timestamp.
	Rest string
}

// Clock turns times of day into a monotonic timeline so logs that cross
// midnight keep increasing instead of jumping back to 00:00.
type Clock struct {
	dayOffset float64
	lastSecs  float64
	started   bool
}

// Stamp converts a time of day (and optional date) into absolute seconds on
// this log's own timeline.
func (c *Clock) Stamp(secs float64, day *time.Time) float64 {
	if day != nil {
		c.dayOffset = float64(ordinal(*day)) * 86400
		c.lastSecs = secs
		c.started = true
		return c.dayOffset + secs
	}
	if c.started && secs < c.lastSecs-12*3600 {
		// Only a large backwards jump means "new day"; small ones are
		// out-of-order lines within the same day.
		c.dayOffset += 86400
	}
	if !c.started || secs > c.lastSecs {
		c.lastSecs = secs
	}
	c.started = true
	return c.dayOffset + secs
}

func ordinal(t time.Time) int {
	y, m, d := t.Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

var (
	// 2024-03-15 18:22:31 / 2024.03.15T18:22 / [2024-03-15 18:22:31,123]
	reISO = regexp.MustCompile(`^[ \t]*\[?(\d{4})[.\-/](\d{1,2})[.\-/](\d{1,2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2}))?(?:[.,]\d+)?)?\]?[ \t]*`)
	// 15.03.2024 18:22:31
	reDMY = regexp.MustCompile(`^[ \t]*\[?(\d{1,2})[.\-/](\d{1,2})[.\-/](\d{4})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2}))?(?:[.,]\d+)?)?\]?[ \t]*`)
	// 18:22:31 / [18:22:31] / 18:22:31.123
	reTimeOnly = regexp.MustCompile(`^[ \t]*\[?(\d{1,2}):(\d{2})(?::(\d{2}))?(?:[.,]\d+)?\]?[ \t]*`)
)

// ParseTimestamp splits a raw log line into its timestamp prefix and message.
func ParseTimestamp(line string) (Timestamp, bool) {
	if groups, rest, ok := matchPrefix(reISO, line); ok {
		day := buildDate(groups[0], groups[1], groups[2])
		return Timestamp{Secs: secsOfDay(groups[3], groups[4], groups[5]), Day: &day, Rest: rest}, true
	}
	if groups, rest, ok := matchPrefix(reDMY, line); ok {
		day := buildDate(groups[2], groups[1], groups[0])
		return Timestamp{Secs: secsOfDay(groups[3], groups[4], groups[5]), Day: &day, Rest: rest}, true
	}
	if groups, rest, ok := matchPrefix(reTimeOnly, line); ok {
		hour := atoi(groups[0])
		if hour > 23 || atoi(groups[1]) > 59 {
			// e.g. a "Class 3:4" prefix, not a clock.
			return Timestamp{}, false
		}
		return Timestamp{Secs: secsOfDay(groups[0], groups[1], groups[2]), Rest: rest}, true
	}
	return Timestamp{}, false
}

// matchPrefix returns the capture groups of an anchored prefix match plus the
// remaining text.
func matchPrefix(re *regexp.Regexp, line string) ([]string, string, bool) {
	idx := re.FindStringSubmatchIndex(line)
	if idx == nil {
		return nil, "", false
	}
	rest := line[idx[1]:]
	groups := make([]string, 0, len(idx)/2-1)
	for i := 2; i < len(idx); i += 2 {
		if idx[i] < 0 {
			groups = append(groups, "")
			continue
		}
		groups = append(groups, line[idx[i]:idx[i+1]])
	}
	return groups, rest, true
}

func buildDate(year, month, day string) time.Time {
	return time.Date(atoi(year), time.Month(atoi(month)), atoi(day), 0, 0, 0, 0, time.UTC)
}

func secsOfDay(h, m, s string) float64 {
	return float64(atoi(h)*3600 + atoi(m)*60 + atoi(s))
}

func atoi(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// group returns capture group i (1-based) or "" when absent.
func group(groups []string, i int) string {
	if i-1 < 0 || i-1 >= len(groups) {
		return ""
	}
	return groups[i-1]
}
