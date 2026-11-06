package logs

import (
	"path"
	"regexp"
	"strings"
)

// Extensions that map straight to a parser.
var typeByExtension = map[string]FileType{
	"adm":   FileADM,
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

// admMarkers are lines only a DayZ admin log produces. Generic ".log" files
// (some hosts rename every log) are only treated as ADM when at least two of
// them match, so an unrelated log is reported as unknown instead of parsed.
var admMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)AdminLog started on`),
	regexp.MustCompile(`(?i)\| Player "`),
	regexp.MustCompile(`(?i)^Player "[^"]{1,80}"(?: \([^)]*\))*\s+(?:is\s+)?connected`),
	regexp.MustCompile(`(?i)killed by Player "[^"]+"`),
	regexp.MustCompile(`(?i)hit by Player "[^"]+"`),
}

// DetectType decides the effective type from the file name plus a content
// sample. Extensions win when present, because hosted providers sometimes
// ship everything as ".log".
func DetectType(name string, sample []string) FileType {
	switch byExt := TypeFromName(name); byExt {
	case FileCrash, FileADM:
		return byExt
	}

	if scoreAdm(sample) >= 2 {
		return FileADM
	}
	return FileUnknown
}

// scoreAdm counts how strongly a content sample looks like an ADM log:
// two points per matching marker.
func scoreAdm(sample []string) int {
	text := strings.Join(sample, "\n")
	score := 0
	for _, re := range admMarkers {
		if re.MatchString(text) {
			score += 2
		}
	}
	return score
}

// IsCandidateEntry reports whether a name inside a profiles folder is worth
// reading (log-like, not a directory marker or dotfile).
func IsCandidateEntry(name string) bool {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	if base == "" || strings.HasPrefix(base, ".") || strings.HasSuffix(name, "/") {
		return false
	}
	switch Extension(base) {
	case "adm", "log", "txt", "mdmp", "crash", "bidmp", "dmp":
		return true
	}
	return false
}
