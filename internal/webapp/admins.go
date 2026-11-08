package webapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dayzctl/dayzctl/internal/logger"
)

// steamAdminID matches the SteamID64 form used in web.auth.steam.admins.
var steamAdminID = regexp.MustCompile(`^7656[0-9]{13}$`)

// errNoConfigPath means the server was given no config file to write to (for
// example when it is embedded in a test).
var errNoConfigPath = errors.New("dayzctl web was started without a config file, so admins cannot be saved")

// adminSet is the live Steam admin list. It starts as a copy of the config and
// is replaced when the web UI grants or revokes an admin, so a change is in
// effect immediately and survives a restart through the config file.
type adminSet struct {
	mu     sync.RWMutex
	admins map[string]string
}

func newAdminSet(initial map[string]string) *adminSet {
	a := &adminSet{admins: make(map[string]string, len(initial))}
	a.set(initial)
	return a
}

// allowed reports whether id is an admin, and the label to show.
func (a *adminSet) allowed(id string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	name, ok := a.admins[strings.TrimSpace(id)]
	if !ok {
		return "", false
	}
	if name == "" {
		return "Steam " + strings.TrimSpace(id), true
	}
	return name, true
}

// snapshot returns a copy safe to hand to the config writer.
func (a *adminSet) snapshot() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]string, len(a.admins))
	for id, name := range a.admins {
		out[id] = name
	}
	return out
}

func (a *adminSet) set(m map[string]string) {
	out := make(map[string]string, len(m))
	for id, name := range m {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out[id] = strings.TrimSpace(name)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.admins = out
}

// steamAdminView is one entry of the admin list in the API.
type steamAdminView struct {
	SteamID string `json:"steamID"`
	Name    string `json:"name,omitempty"`
}

// pendingSteamView is a Steam account that tried to sign in and was refused
// because it is not an admin yet.
type pendingSteamView struct {
	SteamID  string    `json:"steamID"`
	FirstAt  time.Time `json:"firstAt"`
	LastAt   time.Time `json:"lastAt"`
	Attempts int       `json:"attempts"`
}

// pendingSet remembers refused Steam sign-ins so the operator can approve the
// person they just saw at the login page. It is in memory only: a restart
// clears it, which is fine because approving is a two-minute action.
type pendingSet struct {
	mu      sync.Mutex
	entries map[string]pendingSteamView
	ttl     time.Duration
}

func newPendingSet() *pendingSet {
	return &pendingSet{entries: make(map[string]pendingSteamView), ttl: 7 * 24 * time.Hour}
}

func (p *pendingSet) record(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.entries {
		if now.Sub(entry.LastAt) > p.ttl {
			delete(p.entries, key)
		}
	}
	entry := p.entries[id]
	if entry.SteamID == "" {
		entry.SteamID = id
		entry.FirstAt = now
	}
	entry.LastAt = now
	entry.Attempts++
	p.entries[id] = entry
}

// list returns the pending sign-ins, most recent first.
func (p *pendingSet) list() []pendingSteamView {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]pendingSteamView, 0, len(p.entries))
	for _, entry := range p.entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastAt.After(out[j].LastAt) })
	return out
}

func (p *pendingSet) remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, strings.TrimSpace(id))
}

// adminsResponse is what GET /api/admins returns.
type adminsResponse struct {
	// Steam reports whether Steam SSO is enabled at all.
	Steam bool `json:"steam"`
	// Writable reports whether a config file is available to save changes to.
	Writable bool               `json:"writable"`
	Admins   []steamAdminView   `json:"admins"`
	Pending  []pendingSteamView `json:"pending"`
}

func (s *Server) adminsPayload() adminsResponse {
	snapshot := s.admins.snapshot()
	ids := make([]string, 0, len(snapshot))
	for id := range snapshot {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	admins := make([]steamAdminView, 0, len(ids))
	for _, id := range ids {
		admins = append(admins, steamAdminView{SteamID: id, Name: snapshot[id]})
	}
	return adminsResponse{
		Steam:    s.auth.SteamEnabled(),
		Writable: s.configPath != "",
		Admins:   admins,
		Pending:  s.pending.list(),
	}
}

// handleAdmins lists the current Steam admins and the refused sign-ins.
func (s *Server) handleAdmins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.adminsPayload())
}

// handleAdminUpdate grants or revokes Steam admin rights and writes the
// result to the config file, so the change also survives a restart.
func (s *Server) handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SteamID string `json:"steamID"`
		Name    string `json:"name"`
		Action  string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed body")
		return
	}
	id := strings.TrimSpace(body.SteamID)
	if !steamAdminID.MatchString(id) {
		writeError(w, http.StatusBadRequest, "steamID must be a SteamID64 (17 digits starting 7656)")
		return
	}
	if !s.auth.SteamEnabled() {
		writeError(w, http.StatusConflict, "Steam sign-in is disabled (web.auth.steam.enabled)")
		return
	}

	next := s.admins.snapshot()
	switch body.Action {
	case "", "add":
		next[id] = strings.TrimSpace(body.Name)
	case "remove":
		delete(next, id)
	default:
		writeError(w, http.StatusBadRequest, `action must be "add" or "remove"`)
		return
	}

	if err := s.saveSteamAdmins(next); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNoConfigPath) {
			status = http.StatusNotImplemented
		}
		writeError(w, status, err.Error())
		return
	}
	s.pending.remove(id)
	logger.Info("Steam admin updated", "steamID", id, "action", body.Action, "config", s.configPath)
	writeJSON(w, http.StatusOK, s.adminsPayload())
}

// saveSteamAdmins writes the list to the config file and, only when that
// worked, swaps it into the running server.
func (s *Server) saveSteamAdmins(admins map[string]string) error {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	if s.configPath == "" {
		return errNoConfigPath
	}
	if err := writeSteamAdmins(s.configPath, admins); err != nil {
		if errors.Is(err, syscall.EROFS) {
			return fmt.Errorf("config file is read-only: %s (the dayz-web.service sandbox mounts /etc read-only; "+
				"run 'dayzctl apply' to regenerate the unit with ReadWritePaths=/etc/dayzctl)", s.configPath)
		}
		return err
	}
	s.admins.set(admins)
	return nil
}

// writeSteamAdmins edits web.auth.steam.admins in place. The common case is a
// line splice, so comments, blank lines and the rest of the file stay byte for
// byte; only when the block is missing entirely does it fall back to rewriting
// the YAML document (comments survive that too, blank lines do not).
func writeSteamAdmins(path string, admins map[string]string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config file: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	out, ok := spliceAdminsBlock(string(data), admins)
	if ok {
		var check yaml.Node
		if err := yaml.Unmarshal(out, &check); err != nil {
			ok = false // fall back rather than write something unparseable
		}
	}
	if !ok {
		encoded, err := reencodeAdmins(&doc, admins)
		if err != nil {
			return err
		}
		out = encoded
	}
	return replaceFile(path, out, info.Mode().Perm())
}

// spliceAdminsBlock replaces (or inserts) the admins block textually and
// returns the new file plus whether it found a steam block to anchor on.
func spliceAdminsBlock(text string, admins map[string]string) ([]byte, bool) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	steam := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// The top-level steam: block (SteamCMD account) must not win over the
		// nested web.auth.steam: one, so only an indented key counts.
		if trimmed == "steam:" && lineIndent(line) > 0 {
			steam = i
		}
	}
	if steam < 0 {
		return nil, false
	}
	steamIndent := lineIndent(lines[steam])

	// The steam block runs until the next line at the same or lower indent.
	end := len(lines)
	for i := steam + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if lineIndent(lines[i]) <= steamIndent {
			end = i
			break
		}
	}

	start, stop, keyIndent := -1, -1, steamIndent+2
	for i := steam + 1; i < end; i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "admins:") {
			continue
		}
		start, keyIndent = i, lineIndent(lines[i])
		stop = start + 1
		for stop < end {
			trimmed := strings.TrimSpace(lines[stop])
			if trimmed == "" {
				stop++
				continue
			}
			if lineIndent(lines[stop]) > keyIndent {
				stop++
				continue
			}
			break
		}
		for stop > start+1 && strings.TrimSpace(lines[stop-1]) == "" {
			stop--
		}
		break
	}

	entry := strings.Repeat(" ", keyIndent+2)
	block := make([]string, 0, len(admins)+1)
	block = append(block, strings.Repeat(" ", keyIndent)+"admins:")
	for _, id := range sortedKeys(admins) {
		block = append(block, entry+yamlQuote(id)+": "+yamlScalar(admins[id]))
	}

	switch {
	case start >= 0:
		lines = append(append(append([]string{}, lines[:start]...), block...), lines[stop:]...)
	case end < len(lines):
		// Insert before the block that follows steam, keeping its blank line.
		at := end
		for at > steam+1 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		lines = append(append(append([]string{}, lines[:at]...), block...), lines[at:]...)
	default:
		lines = append(lines, block...)
	}

	return []byte(strings.Join(lines, "\n") + "\n"), true
}

func lineIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// yamlScalar quotes a value only when it would otherwise confuse the parser.
func yamlScalar(value string) string {
	safe := value != "" &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, ":#{}[],&*!|>'\"%@`\\\n\r\t") &&
		!strings.HasPrefix(value, "-") &&
		!strings.HasPrefix(value, "?")
	if safe {
		return value
	}
	return yamlQuote(value)
}

// yamlQuote always double-quotes a value. SteamIDs need it: a bare 17-digit
// number is read as an integer by YAML 1.1 parsers.
func yamlQuote(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// reencodeAdmins is the fallback: it rewrites the document from its node tree,
// creating web.auth.steam.admins when it is missing.
func reencodeAdmins(doc *yaml.Node, admins map[string]string) ([]byte, error) {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("config is not a YAML mapping")
	}
	steam := ensureMap(ensureMap(ensureMap(doc.Content[0], "web"), "auth"), "steam")
	setMapEntry(steam, "admins", steamAdminsNode(admins))

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return buf.Bytes(), nil
}

// replaceFile swaps path for data atomically, keeping its permissions.
func replaceFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dayzctl-config-*.yaml")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// steamAdminsNode builds the mapping written under web.auth.steam.admins.
func steamAdminsNode(admins map[string]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	ids := make([]string, 0, len(admins))
	for id := range admins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: id},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: admins[id]},
		)
	}
	return node
}

// ensureMap returns the mapping stored under key, creating it (or replacing a
// null/other value) when it is missing.
func ensureMap(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		value := parent.Content[i+1]
		if value.Kind != yaml.MappingNode {
			value.Kind = yaml.MappingNode
			value.Tag = "!!map"
			value.Value = ""
			value.Content = nil
		}
		return value
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
	return value
}

// setMapEntry replaces the value of key, keeping the key node so its comment
// stays attached.
func setMapEntry(parent *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = value
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}
