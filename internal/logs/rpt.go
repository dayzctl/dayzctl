package logs

import (
	"regexp"
	"sort"
	"strings"
)

const (
	maxGroups     = 4000
	maxSamples    = 3
	maxSampleText = 300
)

var (
	reNoiseLine = regexp.MustCompile(`^[-=*_~#\s]{3,}$`)
	rePathLike  = regexp.MustCompile(`(?i)(?:[A-Za-z]:\\[^\s"']+|[\w\\/.-]+\.(?:p3d|paa|ogg|wss|rvmat|cpp|xml|bin|sqf))\b`)
	reQuoted    = regexp.MustCompile(`'[^']{0,120}'|"[^"]{0,120}"`)
	reHex       = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reNumber    = regexp.MustCompile(`\b\d+(?:[.,]\d+)?\b`)
	reSpaces    = regexp.MustCompile(`\s+`)
	reAtMod     = regexp.MustCompile(`@([\w-]{2,40})`)
	reAddonName = regexp.MustCompile(`Addon '([^']{2,60})'`)
	reCrashMark = regexp.MustCompile(`(?i)Exception code|Application crashed|Access violation`)
	reEndMark   = regexp.MustCompile(`(?i)\b(?:Exiting|Shutdown|Mission read|Server stopped|terminated)\b`)
)

// SeverityCounts is the occurrence tally per severity.
type SeverityCounts struct {
	Critical int `json:"critical"`
	Error    int `json:"error"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

func (c *SeverityCounts) add(s Severity) {
	switch s {
	case SeverityCritical:
		c.Critical++
	case SeverityError:
		c.Error++
	case SeverityWarning:
		c.Warning++
	default:
		c.Info++
	}
}

// Merge adds another tally.
func (c *SeverityCounts) Merge(other SeverityCounts) {
	c.Critical += other.Critical
	c.Error += other.Error
	c.Warning += other.Warning
	c.Info += other.Info
}

// Total returns the number of occurrences across all severities.
func (c SeverityCounts) Total() int {
	return c.Critical + c.Error + c.Warning + c.Info
}

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityError:
		return 1
	case SeverityWarning:
		return 2
	default:
		return 3
	}
}

// RptGroup is one distinct engine message pattern with its explanation.
type RptGroup struct {
	Key      string   `json:"key"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Explain  string   `json:"explain"`
	Fix      string   `json:"fix"`
	Vanilla  bool     `json:"vanilla"`
	Category string   `json:"category"`
	RuleID   string   `json:"ruleId"`
	Count    int      `json:"count"`
	FirstTod *float64 `json:"firstTod"`
	LastTod  *float64 `json:"lastTod"`
	Samples  []string `json:"samples"`
	Sample   string   `json:"sample"`
}

// RptResult is the merged outcome of one or more RPT files.
type RptResult struct {
	Groups            []RptGroup     `json:"groups"`
	Counts            SeverityCounts `json:"counts"`
	Mods              []string       `json:"mods"`
	EndState          string         `json:"endState"`
	HasDate           bool           `json:"hasDate"`
	LineCount         int            `json:"lineCount"`
	MessageCount      int            `json:"messageCount"`
	OverflowGroups    int            `json:"overflowGroups"`
	MinTod            *float64       `json:"minTod"`
	MaxTod            *float64       `json:"maxTod"`
	ProblemCount      int            `json:"problemCount"`
	ProblemGroupCount int            `json:"problemGroupCount"`
	GroupCount        int            `json:"groupCount"`
}

// NormalizeMessage collapses volatile details so repeats share one group key.
func NormalizeMessage(message string) string {
	s := rePathLike.ReplaceAllString(message, "<path>")
	s = reQuoted.ReplaceAllString(s, "<q>")
	s = reHex.ReplaceAllString(s, "<hex>")
	s = reNumber.ReplaceAllString(s, "<n>")
	s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
	s = strings.ToLower(s)
	if len(s) > 260 {
		s = s[:260]
	}
	return s
}

// RptParser accumulates classified engine messages.
type RptParser struct {
	clock Clock

	groups map[string]*RptGroup
	order  []string

	LineCount      int
	MessageCount   int
	OverflowGroups int
	HasDate        bool
	Counts         SeverityCounts

	minTod, maxTod float64
	hasTod         bool

	sampleLines []string
	mods        map[string]struct{}
	sawCrash    bool
	sawShutdown bool

	lastT   float64
	lastTod *float64
}

// NewRptParser returns an empty parser.
func NewRptParser() *RptParser {
	return &RptParser{
		groups: make(map[string]*RptGroup),
		mods:   make(map[string]struct{}),
	}
}

// Feed classifies one line.
func (p *RptParser) Feed(line string) {
	p.LineCount++
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || reNoiseLine.MatchString(trimmed) {
		return
	}

	tod := p.lastTod
	t := p.lastT

	ts, hasTS := ParseTimestamp(line)
	if hasTS {
		tod = floatPtr(ts.Secs)
		if ts.Day != nil {
			p.HasDate = true
		}
		t = p.clock.Stamp(ts.Secs, ts.Day)
		if !p.hasTod {
			p.minTod, p.maxTod = ts.Secs, ts.Secs
			p.hasTod = true
		} else {
			if ts.Secs < p.minTod {
				p.minTod = ts.Secs
			}
			if ts.Secs > p.maxTod {
				p.maxTod = ts.Secs
			}
		}
	}

	message := trimmed
	if hasTS {
		message = strings.TrimSpace(reLeadingPipe.ReplaceAllString(strings.TrimSpace(ts.Rest), ""))
	}
	if message == "" {
		return
	}

	p.lastT = t
	p.lastTod = tod
	p.MessageCount++

	if len(p.sampleLines) < 60 {
		p.sampleLines = append(p.sampleLines, trimmed)
	}

	if strings.Contains(message, "@") {
		for _, m := range reAtMod.FindAllStringSubmatch(message, -1) {
			p.mods[m[1]] = struct{}{}
		}
	}
	for _, m := range reAddonName.FindAllStringSubmatch(message, -1) {
		p.mods[m[1]] = struct{}{}
	}

	verdict := ClassifyMessage(message)
	p.Counts.add(verdict.Severity)

	if reCrashMark.MatchString(message) {
		p.sawCrash = true
	}
	if reEndMark.MatchString(message) {
		p.sawShutdown = true
	}

	key := string(verdict.Severity) + "|" + verdict.RuleID
	if verdict.RuleID == "" {
		key = string(verdict.Severity) + "|" + NormalizeMessage(message)
	}

	g, ok := p.groups[key]
	if !ok {
		if len(p.groups) >= maxGroups {
			p.OverflowGroups++
			return
		}
		sample := message
		if len(sample) > maxSampleText {
			sample = sample[:maxSampleText]
		}
		g = &RptGroup{
			Key:      key,
			Severity: verdict.Severity,
			Title:    verdict.Title,
			Explain:  verdict.Explain,
			Fix:      verdict.Fix,
			Vanilla:  verdict.Vanilla,
			Category: verdict.Category,
			RuleID:   verdict.RuleID,
			Sample:   sample,
			Samples:  []string{},
		}
		p.groups[key] = g
		p.order = append(p.order, key)
	}

	g.Count++
	if tod != nil {
		if g.FirstTod == nil || *tod < *g.FirstTod {
			g.FirstTod = tod
		}
		if g.LastTod == nil || *tod > *g.LastTod {
			g.LastTod = tod
		}
	}
	if len(g.Samples) < maxSamples && !containsString(g.Samples, g.Sample) {
		raw := message
		if len(raw) > maxSampleText {
			raw = raw[:maxSampleText]
		}
		g.Samples = append(g.Samples, raw)
	}
}

// Finish returns the result for this file.
func (p *RptParser) Finish() RptResult {
	groups := make([]RptGroup, 0, len(p.groups))
	for _, key := range p.order {
		groups = append(groups, *p.groups[key])
	}
	sort.SliceStable(groups, func(i, j int) bool {
		ri, rj := severityRank(groups[i].Severity), severityRank(groups[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return groups[i].Count > groups[j].Count
	})

	endState := "unknown"
	if p.sawCrash {
		endState = "crash"
	} else if p.sawShutdown {
		endState = "clean"
	}

	mods := make([]string, 0, len(p.mods))
	for m := range p.mods {
		mods = append(mods, m)
	}
	sort.Strings(mods)

	var minTod, maxTod *float64
	if p.hasTod {
		minTod = floatPtr(p.minTod)
		maxTod = floatPtr(p.maxTod)
	}

	return RptResult{
		Groups:         groups,
		Counts:         p.Counts,
		Mods:           mods,
		EndState:       endState,
		HasDate:        p.HasDate,
		LineCount:      p.LineCount,
		MessageCount:   p.MessageCount,
		OverflowGroups: p.OverflowGroups,
		MinTod:         minTod,
		MaxTod:         maxTod,
	}
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// MergeRpt combines several parse results into one report block.
func MergeRpt(results []RptResult) RptResult {
	merged := make(map[string]*RptGroup)
	order := make([]string, 0, 16)
	var counts SeverityCounts
	mods := make(map[string]struct{})
	var lineCount, messageCount, overflow int
	var hasDate, sawCrash, sawClean bool
	var minTod, maxTod *float64

	for _, r := range results {
		counts.Merge(r.Counts)
		lineCount += r.LineCount
		messageCount += r.MessageCount
		overflow += r.OverflowGroups
		hasDate = hasDate || r.HasDate
		for _, m := range r.Mods {
			mods[m] = struct{}{}
		}
		switch r.EndState {
		case "crash":
			sawCrash = true
		case "clean":
			sawClean = true
		}
		if r.MinTod != nil && (minTod == nil || *r.MinTod < *minTod) {
			minTod = r.MinTod
		}
		if r.MaxTod != nil && (maxTod == nil || *r.MaxTod > *maxTod) {
			maxTod = r.MaxTod
		}

		for i := range r.Groups {
			g := r.Groups[i]
			existing, ok := merged[g.Key]
			if !ok {
				copied := g
				copied.Samples = append([]string{}, g.Samples...)
				merged[g.Key] = &copied
				order = append(order, g.Key)
				continue
			}
			existing.Count += g.Count
			if g.FirstTod != nil && (existing.FirstTod == nil || *g.FirstTod < *existing.FirstTod) {
				existing.FirstTod = g.FirstTod
			}
			if g.LastTod != nil && (existing.LastTod == nil || *g.LastTod > *existing.LastTod) {
				existing.LastTod = g.LastTod
			}
			for _, s := range g.Samples {
				if len(existing.Samples) < maxSamples && !containsString(existing.Samples, s) {
					existing.Samples = append(existing.Samples, s)
				}
			}
		}
	}

	groups := make([]RptGroup, 0, len(merged))
	for _, key := range order {
		groups = append(groups, *merged[key])
	}
	sort.SliceStable(groups, func(i, j int) bool {
		ri, rj := severityRank(groups[i].Severity), severityRank(groups[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return groups[i].Count > groups[j].Count
	})

	modList := make([]string, 0, len(mods))
	for m := range mods {
		modList = append(modList, m)
	}
	sort.Strings(modList)

	endState := "unknown"
	if sawCrash {
		endState = "crash"
	} else if sawClean {
		endState = "clean"
	}

	problemGroups := 0
	for _, g := range groups {
		if g.Severity != SeverityInfo {
			problemGroups++
		}
	}

	return RptResult{
		Groups:            groups,
		Counts:            counts,
		Mods:              modList,
		EndState:          endState,
		HasDate:           hasDate,
		LineCount:         lineCount,
		MessageCount:      messageCount,
		OverflowGroups:    overflow,
		MinTod:            minTod,
		MaxTod:            maxTod,
		ProblemCount:      counts.Critical + counts.Error + counts.Warning,
		ProblemGroupCount: problemGroups,
		GroupCount:        len(groups),
	}
}
