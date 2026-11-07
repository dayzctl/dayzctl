// Package banhints remembers which player a ban GUID belongs to.
//
// DayZ's ban list only carries BattlEye's 32-hex GUID, while the ADM log
// records either a SteamID (older builds) or a per-session token (current
// builds). The two cannot be derived from each other, so the link is recorded
// when it is known (a ban placed from the web UI, or a banned GUID still seen
// in the live player list) and persisted here. The web UI uses it to put the
// Unban button on the right player row; the CLI uses it to resolve a name.
package banhints

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

// Store is the persisted GUID → player name map.
type Store struct {
	mu     sync.Mutex
	path   string
	byGUID map[string]string
}

// New loads the map from path; a missing or unreadable file starts empty.
func New(path string) *Store {
	s := &Store{path: path, byGUID: make(map[string]string)}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("Cannot read ban hints", "path", path, "error", err)
		}
		return s
	}
	if err := json.Unmarshal(data, &s.byGUID); err != nil {
		logger.Warn("Ignoring unreadable ban hints file", "path", path, "error", err)
		s.byGUID = make(map[string]string)
	}
	return s
}

// Path is the state file: <state dir>/ban-hints.json.
func Path(cfg *config.ServerConfig) string {
	if cfg == nil {
		return ""
	}
	return filepath.Join(cfg.GetStateDir(), "ban-hints.json")
}

// NormalizeGUID is the key format used by the map: lower-case, trimmed.
func NormalizeGUID(guid string) string {
	return strings.ToLower(strings.TrimSpace(guid))
}

// NormalizeName compares player names case-insensitively and without the
// surrounding quotes some server builds print.
func NormalizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, `"'`)
	return strings.ToLower(name)
}

// Player returns the player name recorded for a ban GUID.
func (s *Store) Player(guid string) string {
	guid = NormalizeGUID(guid)
	if guid == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byGUID[guid]
}

// GUIDSByName returns every ban GUID recorded for a player name.
func (s *Store) GUIDSByName(name string) []string {
	want := NormalizeName(name)
	if want == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []string
	for guid, stored := range s.byGUID {
		if NormalizeName(stored) == want {
			out = append(out, guid)
		}
	}
	return out
}

// Remember records (and persists) the GUID → name link.
func (s *Store) Remember(guid, name string) {
	guid = NormalizeGUID(guid)
	name = strings.TrimSpace(name)
	if guid == "" || name == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byGUID[guid] == name {
		return
	}
	s.byGUID[guid] = name
	s.saveLocked()
}

// Forget drops a hint once its ban is gone.
func (s *Store) Forget(guid string) {
	guid = NormalizeGUID(guid)
	if guid == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byGUID[guid]; !ok {
		return
	}
	delete(s.byGUID, guid)
	s.saveLocked()
}

// Prune drops hints whose ban no longer exists. The caller passes the GUIDs
// still present in the live ban list.
func (s *Store) Prune(live map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for guid := range s.byGUID {
		if _, ok := live[guid]; !ok {
			delete(s.byGUID, guid)
			changed = true
		}
	}
	if changed {
		s.saveLocked()
	}
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		logger.Warn("Cannot create ban hints directory", "path", s.path, "error", err)
		return
	}
	data, err := json.MarshalIndent(s.byGUID, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logger.Warn("Cannot write ban hints", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		logger.Warn("Cannot replace ban hints", "path", s.path, "error", err)
	}
}
