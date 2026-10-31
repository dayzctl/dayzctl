package logs

import (
	"path"
	"regexp"
	"strings"
)

// Extensions that map straight to a parser.
var typeByExtension = map[string]FileType{
	"adm":   FileADM,
	"rpt":   FileRPT,
	"mdmp":  FileCrash,
	"bidmp": FileCrash,
	"dmp":   FileCrash,
	"crash": FileCrash,
}

// Extension returns the lowercase extension of a path, without the dot.
func Extension(name string) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	dot := strings.LastIndex(base, ".")
	if dot <= 0 {
		return ""
	}
	return strings.ToLower(base[dot+1:])
}

// TypeFromName guesses the type from the file extension alone.
func TypeFromName(name string) FileType {
	if t, ok := typeByExtension[Extension(name)]; ok {
		return t
	}
	return FileUnknown
}

var (
	// Markers that only appear in a DayZ admin log.
	admMarkers = []*regexp.Regexp{
		regexp.MustCompile(`(?i)AdminLog started on`),
		regexp.MustCompile(`(?i)\| Player "`),
		regexp.MustCompile(`(?i)^Player "[^"]{1,80}"(?: \([^)]*\))*\s+(?:is\s+)?connected`),
		regexp.MustCompile(`(?i)killed by Player "[^"]+"`),
		regexp.MustCompile(`(?i)hit by Player "[^"]+"`),
	}

	// Markers from a real DayZ server RPT. Deliberately specific: a generic
	// "ERROR:" line must not make an unrelated .log look like a DayZ report.
	rptMarkers = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^Exe timestamp:`),
		regexp.MustCompile(`(?i)^Current time:`),
		regexp.MustCompile(`(?i)Average server FPS:`),
		regexp.MustCompile(`(?i)^Used memory: [\d.]+ KB`),
		regexp.MustCompile(`(?i)^Players: \d+ in total`),
		regexp.MustCompile(`(?i)\bENTITY\s+(?:\([WE]\)\s*)?:`),
		regexp.MustCompile(`(?i)^\[CE\]`),
		regexp.MustCompile(`(?i)DayZServer`),
		regexp.MustCompile(`(?i)Strange convex component`),
		regexp.MustCompile(`(?i)Cannot open object`),
		regexp.MustCompile(`(?i)Addon '[^']+' requires addon '[^']+'`),
		regexp.MustCompile(`(?i)\.p3d\b`),
		regexp.MustCompile(`(?i)Dedicated Server`),
	}
	reScriptLine = regexp.MustCompile(`^\s*SCRIPT\s+:`)
)

// DetectType decides the effective type from the file name plus a content
// sample. Extensions win when present, because hosted providers sometimes
// ship everything as ".log".
func DetectType(name string, sample []string) FileType {
	switch byExt := TypeFromName(name); byExt {
	case FileCrash, FileADM, FileRPT:
		return byExt
	}

	admScore, rptScore, scriptScore := scoreSample(sample)
	if scriptScore > 0 && scriptScore >= admScore && scriptScore >= rptScore {
		return FileScript
	}
	// Content detection needs at least one DayZ-specific marker; otherwise the
	// file is reported as unknown instead of being half-parsed as an RPT.
	if admScore >= 2 && admScore > rptScore {
		return FileADM
	}
	if rptScore >= 2 {
		return FileRPT
	}
	return FileUnknown
}

// scoreSample counts how strongly a content sample looks like each file type:
// two points per matching ADM/RPT marker, three for a script line.
func scoreSample(sample []string) (adm, rpt, script int) {
	text := strings.Join(sample, "\n")
	for _, re := range admMarkers {
		if re.MatchString(text) {
			adm += 2
		}
	}
	for _, re := range rptMarkers {
		if re.MatchString(text) {
			rpt += 2
		}
	}
	for _, line := range sample {
		if reScriptLine.MatchString(line) {
			script += 3
			break
		}
	}
	return adm, rpt, script
}

// RPTOrigin distinguishes a server RPT from a player's game-client RPT.
type RPTOrigin string

const (
	OriginServer  RPTOrigin = "server"
	OriginClient  RPTOrigin = "client"
	OriginUnknown RPTOrigin = "unknown"
)

var (
	serverSignals = []*regexp.Regexp{
		regexp.MustCompile(`(?i)DayZServer`),
		regexp.MustCompile(`(?i)-profiles=`),
		regexp.MustCompile(`(?i)Dedicated Server`),
		regexp.MustCompile(`(?i)BattlEye Server`),
		regexp.MustCompile(`(?i)Mission [^\n]*read`),
		regexp.MustCompile(`(?i)Player "[^"]+" is connected`),
		regexp.MustCompile(`(?i)^Host: `),
		regexp.MustCompile(`(?i)^Network: `),
	}
	clientSignals = []*regexp.Regexp{
		regexp.MustCompile(`(?i)Graphics:|GPU:|Adapters:|DXGI|D3D11`),
		regexp.MustCompile(`(?i)Client hasn't been initialised`),
		regexp.MustCompile(`(?i)Connect to server`),
	}
)

// ClassifyRPTOrigin reports whether an RPT looks like server or client output.
func ClassifyRPTOrigin(text string) RPTOrigin {
	server, client := 0, 0
	for _, re := range serverSignals {
		if re.MatchString(text) {
			server++
		}
	}
	for _, re := range clientSignals {
		if re.MatchString(text) {
			client++
		}
	}
	switch {
	case server > client:
		return OriginServer
	case client > server:
		return OriginClient
	default:
		return OriginUnknown
	}
}

// IsCandidateEntry reports whether a name inside a profiles folder is worth
// reading (log-like, not a directory marker or dotfile).
func IsCandidateEntry(name string) bool {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	if base == "" || strings.HasPrefix(base, ".") || strings.HasSuffix(name, "/") {
		return false
	}
	switch Extension(base) {
	case "adm", "rpt", "log", "txt", "mdmp", "crash", "bidmp", "dmp":
		return true
	}
	return false
}
