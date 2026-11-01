package logs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	headLines  = 120
	maxLineLen = 1024 * 1024
	maxCached  = 8
)

// LogFile describes one log file in an instance's profiles folder.
type LogFile struct {
	Name    string    `json:"name"`
	Type    FileType  `json:"type"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Service discovers and analyses the log files of DayZ instances.
//
// Analysis results are cached by file signature (name, size, mtime) so
// reloading the dashboard does not re-parse hundreds of megabytes.
type Service struct {
	installDir string
	opts       Options

	mu    sync.Mutex
	cache map[string]*Report
	order []string
}

// NewService returns a service rooted at the DayZ install directory.
func NewService(installDir string, opts Options) *Service {
	if opts == (Options{}) {
		opts = DefaultOptions()
	}
	return &Service{
		installDir: installDir,
		opts:       opts,
		cache:      make(map[string]*Report),
	}
}

// ProfilesDir is where DayZ writes script_*.ADM / script_*.RPT for an
// instance. It matches the -profiles= value written into the systemd unit.
func (s *Service) ProfilesDir(instance string) string {
	return filepath.Join(s.installDir, "profiles", instance)
}

// ListFiles returns the log-like files in an instance's profiles folder,
// newest first.
func (s *Service) ListFiles(instance string) ([]LogFile, error) {
	dir := s.ProfilesDir(instance)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("profiles directory not found: %s", dir)
		}
		return nil, fmt.Errorf("read profiles directory: %w", err)
	}

	files := make([]LogFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !IsCandidateEntry(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fileType := TypeFromName(entry.Name())
		if fileType == FileUnknown {
			fileType = sniffType(filepath.Join(dir, entry.Name()))
			if fileType == FileUnknown {
				// A profiles folder also holds random .txt/.log files; do not
				// pretend they are engine logs.
				continue
			}
		}
		files = append(files, LogFile{
			Name:    entry.Name(),
			Type:    fileType,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files, nil
}

// sniffType reads the head of a file to classify generic ".log" files.
func sniffType(path string) FileType {
	f, err := os.Open(path)
	if err != nil {
		return FileUnknown
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineLen)
	sample := make([]string, 0, 40)
	for len(sample) < 40 && sc.Scan() {
		sample = append(sample, sc.Text())
	}
	// A read error just means fewer sample lines; the type is still sniffed.
	_ = sc.Err()
	detected := DetectType(path, sample)
	return detected
}

// Analyze parses the named files (or every log file when names is empty) and
// returns the report. Results are cached until a file's size or mtime changes.
func (s *Service) Analyze(instance string, names []string) (*Report, error) {
	files, err := s.resolveFiles(instance, names)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no log files found in %s", s.ProfilesDir(instance))
	}

	key := cacheKey(instance, files)
	s.mu.Lock()
	if cached, ok := s.cache[key]; ok {
		s.mu.Unlock()
		hit := *cached
		hit.FromCache = true
		return &hit, nil
	}
	s.mu.Unlock()

	report, err := s.analyze(instance, files)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[key] = report
	s.order = append(s.order, key)
	for len(s.order) > maxCached {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.cache, oldest)
	}
	s.mu.Unlock()

	out := *report
	return &out, nil
}

type resolvedFile struct {
	name    string
	path    string
	fileTyp FileType
	size    int64
	modTime time.Time
}

func (s *Service) resolveFiles(instance string, names []string) ([]resolvedFile, error) {
	dir := s.ProfilesDir(instance)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("profiles directory not found: %s", dir)
		}
		return nil, err
	}

	var selected []string
	if len(names) == 0 {
		listed, err := s.ListFiles(instance)
		if err != nil {
			return nil, err
		}
		for _, f := range listed {
			selected = append(selected, f.Name)
		}
		sort.Strings(selected)
	} else {
		selected = names
	}

	out := make([]resolvedFile, 0, len(selected))
	for _, name := range selected {
		// Reject anything that escapes the profiles directory.
		clean := filepath.Base(strings.ReplaceAll(name, `\`, "/"))
		if clean != name || clean == "." || clean == ".." {
			return nil, fmt.Errorf("invalid file name: %q", name)
		}
		path := filepath.Join(dir, clean)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", clean, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s is a directory", clean)
		}
		fileType := TypeFromName(clean)
		out = append(out, resolvedFile{
			name: clean, path: path, fileTyp: fileType,
			size: info.Size(), modTime: info.ModTime(),
		})
	}
	return out, nil
}

func cacheKey(instance string, files []resolvedFile) string {
	var b strings.Builder
	b.WriteString(instance)
	for _, f := range files {
		fmt.Fprintf(&b, "|%s:%d:%d", f.name, f.size, f.modTime.UnixNano())
	}
	return b.String()
}

func (s *Service) analyze(instance string, files []resolvedFile) (*Report, error) {
	adm := &AdmParser{}
	var rptResults []RptResult
	var rptSamples []string

	report := &Report{
		Instance:    instance,
		Files:       make([]FileInfo, 0, len(files)),
		GeneratedAt: time.Now().UTC(),
	}

	for _, file := range files {
		info := FileInfo{Name: file.name, Type: file.fileTyp, Bytes: file.size}

		if file.fileTyp == FileCrash {
			info.Error = "binary crash dump — not parsed"
			report.Files = append(report.Files, info)
			continue
		}

		f, err := os.Open(file.path)
		if err != nil {
			info.Error = err.Error()
			report.Files = append(report.Files, info)
			continue
		}

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), maxLineLen)

		head := make([]string, 0, headLines)
		for len(head) < headLines && sc.Scan() {
			head = append(head, sc.Text())
		}

		detected := DetectType(file.name, head)
		if detected == FileUnknown {
			info.Type = FileUnknown
			info.Error = "unrecognised file — skipped"
			_ = f.Close()
			report.Files = append(report.Files, info)
			continue
		}
		info.Type = detected

		switch detected {
		case FileADM:
			report.Stats.AdmFiles++
			// Anchor this file to its own day, so a profiles folder holding
			// several rotated logs merges on a real timeline.
			if day, ok := baseDateFor(file.name, head); ok {
				adm.SetBaseDate(day)
			}
			before := adm.LineCount
			for _, line := range head {
				adm.Feed(line)
			}
			for sc.Scan() {
				adm.Feed(sc.Text())
			}
			info.Lines = adm.LineCount - before

		case FileRPT, FileScript:
			report.Stats.RptFiles++
			parser := NewRptParser()
			for _, line := range head {
				parser.Feed(line)
			}
			for sc.Scan() {
				parser.Feed(sc.Text())
			}
			info.Lines = parser.LineCount
			rptResults = append(rptResults, parser.Finish())
			rptSamples = append(rptSamples, strings.Join(head, "\n"))
		}

		if err := sc.Err(); err != nil {
			info.Error = err.Error()
		}
		_ = f.Close()

		if info.Lines == 0 && detected != FileADM {
			info.Lines = len(head)
		}
		report.Files = append(report.Files, info)
	}

	report.Stats.AdmEvents = len(adm.Events)
	report.Stats.AdmUnmatched = adm.Unmatched

	if len(adm.Events) > 0 {
		opts := s.opts
		report.Adm = AnalyzeAdm(adm.Events, opts)
		report.Adm.Period.HasDate = adm.HasDate
	}

	if len(rptResults) > 0 {
		merged := MergeRpt(rptResults)
		report.Rpt = &merged
		report.RptOrigin = ClassifyRPTOrigin(strings.Join(rptSamples, "\n"))
	} else {
		report.RptOrigin = OriginUnknown
	}

	return report, nil
}

// reDateInName finds the YYYY-MM-DD stamp DayZ puts in log file names, e.g.
// DayZServer_2026-10-26_01-20-56.ADM.
var reDateInName = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)

// baseDateFor works out which calendar day a log file belongs to, from its
// name or its "AdminLog started on …" header.
func baseDateFor(name string, head []string) (time.Time, bool) {
	if m := reDateInName.FindStringSubmatch(name); m != nil {
		if day, err := time.Parse("2006-01-02", m[1]+"-"+m[2]+"-"+m[3]); err == nil {
			return day, true
		}
	}
	for _, line := range head {
		if m := reAdminLogStart.FindStringSubmatch(line); m != nil {
			if day, err := time.Parse("2006-01-02", m[1]); err == nil {
				return day, true
			}
		}
	}
	return time.Time{}, false
}

// InvalidateCache drops cached reports, e.g. after a log rotation.
func (s *Service) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]*Report)
	s.order = nil
}
