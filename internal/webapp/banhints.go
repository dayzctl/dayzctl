package webapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

// banHints remembers which player a ban GUID belongs to.
//
// The ban list only carries BattlEye's 32-hex GUID, while the ADM log writes
// either a SteamID (older builds) or a per-session token (current builds), so
// the two cannot be matched after the fact. The only moment both the GUID and
// the name are known is when the ban is issued while the player is connected,
// so it is recorded here and the UI can put the Unban button on that row.
type banHints struct {
	mu     sync.Mutex
	path   string
	byGUID map[string]string
}

func newBanHints(path string) *banHints {
	h := &banHints{path: path, byGUID: make(map[string]string)}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &h.byGUID); err != nil {
			logger.Warn("Ignoring unreadable ban hints file", "path", path, "error", err)
			h.byGUID = make(map[string]string)
		}
	}
	return h
}

// banHintsPath is the state file, under the configured state dir when there is
// one, otherwise next to the instance profiles (which the service can write).
func banHintsPath(cfg *config.ServerConfig) string {
	dir := strings.TrimSpace(cfg.Paths.StateDir)
	switch {
	case dir == "":
		dir = filepath.Join(cfg.GetInstallDir(), ".dayzctl")
	case !filepath.IsAbs(dir):
		dir = filepath.Join(cfg.GetBaseDir(), dir)
	}
	return filepath.Join(dir, "ban-hints.json")
}

// player returns the name a ban GUID was last seen with.
func (h *banHints) player(guid string) string {
	guid = strings.ToLower(strings.TrimSpace(guid))
	if guid == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.byGUID[guid]
}

// remember records (and persists) the GUID → name link.
func (h *banHints) remember(guid, name string) {
	guid = strings.ToLower(strings.TrimSpace(guid))
	name = strings.TrimSpace(name)
	if guid == "" || name == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byGUID[guid] == name {
		return
	}
	h.byGUID[guid] = name
	h.saveLocked()
}

// forget drops a hint once its ban is gone.
func (h *banHints) forget(guid string) {
	guid = strings.ToLower(strings.TrimSpace(guid))
	if guid == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.byGUID[guid]; !ok {
		return
	}
	delete(h.byGUID, guid)
	h.saveLocked()
}

func (h *banHints) saveLocked() {
	if h.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o750); err != nil {
		logger.Warn("Cannot create ban hints directory", "path", h.path, "error", err)
		return
	}
	data, err := json.MarshalIndent(h.byGUID, "", "  ")
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logger.Warn("Cannot write ban hints", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, h.path); err != nil {
		logger.Warn("Cannot replace ban hints", "path", h.path, "error", err)
	}
}
