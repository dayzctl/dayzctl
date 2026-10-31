// Package webapp serves the dayzctl web UI and its JSON API.
//
// The API is deliberately small: list instances, list an instance's log files,
// and analyse a selection of them. All parsing happens server-side in
// internal/logs, so the browser only renders the report it receives.
package webapp

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/gamemap"
	"github.com/dayzctl/dayzctl/internal/logger"
	"github.com/dayzctl/dayzctl/internal/logs"
	"github.com/dayzctl/dayzctl/internal/version"
	webassets "github.com/dayzctl/dayzctl/web"
)

// Headers and messages reused across handlers.
const (
	headerCacheControl  = "Cache-Control"
	headerContentType   = "Content-Type"
	errInstanceNotFound = "instance not found: "
)

// Server bundles the HTTP handler with its dependencies.
type Server struct {
	cfg      *config.ServerConfig
	service  *logs.Service
	maps     *gamemap.Store
	auth     config.WebAuth
	sessions *sessions
	handler  http.Handler
}

// New builds the handler tree for the given configuration.
func New(cfg *config.ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config not loaded")
	}

	s := &Server{
		cfg:      cfg,
		service:  logs.NewService(cfg.GetInstallDir(), logs.DefaultOptions()),
		auth:     cfg.Web.Auth,
		sessions: newSessions(cfg.Web.Auth.SessionDuration()),
	}

	ui, err := fs.Sub(webassets.Assets, "log-analyzer")
	if err != nil {
		return nil, err
	}

	// Map data is read from disk when a directory is configured (the .deb
	// installs it under /usr/share/dayzctl/maps), otherwise from the copy
	// embedded in the binary. Nothing is fetched at runtime either way.
	var embeddedMaps fs.FS
	if sub, err := fs.Sub(webassets.Assets, "log-analyzer/maps"); err == nil {
		embeddedMaps = sub
	}
	s.maps = gamemap.NewLayered(mapsLayer(cfg.GetMapsDir()), embeddedMaps)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/instances", s.handleInstances)
	mux.HandleFunc("GET /api/instances/{instance}/logs", s.handleLogs)
	mux.HandleFunc("GET /api/instances/{instance}/map-image", s.handleMapImage)
	mux.HandleFunc("GET /api/maps", s.handleMaps)
	mux.HandleFunc("GET /api/maps/{map}/meta", s.handleMapMeta)
	mux.HandleFunc("GET /api/maps/{map}/tiles/{z}/{x}/{y}", s.handleMapTile)
	mux.HandleFunc("POST /api/instances/{instance}/analyze", s.handleAnalyze)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.Handle("GET /", noCacheHTML(http.FileServerFS(ui)))

	s.handler = withLogging(withGzip(s.requireAuth(mux)))
	return s, nil
}

// emptyFS stands in for the map tree when no map is available at all.
type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

// mapsLayer returns the on-disk map directory as an fs.FS, or nil when the
// directory is unset or missing so the caller can fall back to embedded data.
func mapsLayer(dir string) fs.FS {
	if dir == "" {
		return nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	return os.DirFS(dir)
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Service exposes the log analysis service (used by tests and the CLI).
func (s *Server) Service() *logs.Service { return s.service }

// Maps lists the map ids bundled with this binary.
func (s *Server) Maps() []string { return s.maps.IDs() }

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Time      string `json:"time"`
	Instances int    `json:"instances"`
	// Auth tells the UI to offer a sign-out button.
	Auth bool `json:"auth,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:    "ok",
		Version:   version.Version,
		Time:      time.Now().UTC().Format(time.RFC3339),
		Instances: len(s.cfg.Instances),
		Auth:      s.auth.Active(),
	})
}

type instanceView struct {
	Name        string     `json:"name"`
	Port        int        `json:"port"`
	Enabled     bool       `json:"enabled"`
	ProfilesDir string     `json:"profilesDir"`
	LogFiles    int        `json:"logFiles"`
	Latest      *time.Time `json:"latest,omitempty"`
	MapImage    string     `json:"mapImage,omitempty"`
	Map         string     `json:"map,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	views := make([]instanceView, 0, len(s.cfg.Instances))
	for i := range s.cfg.Instances {
		inst := &s.cfg.Instances[i]
		view := instanceView{
			Name:        inst.Name,
			Port:        inst.Port,
			Enabled:     inst.Enabled,
			ProfilesDir: s.service.ProfilesDir(inst.Name),
		}
		if files, err := s.service.ListFiles(inst.Name); err == nil {
			view.LogFiles = len(files)
			if len(files) > 0 {
				latest := files[0].ModTime
				view.Latest = &latest
			}
		} else {
			view.Error = err.Error()
		}
		if s.mapImageAvailable(inst) {
			view.MapImage = "/api/instances/" + url.PathEscape(inst.Name) + "/map-image"
		}
		// Only advertise a map that is actually bundled with this binary.
		if id := mapIDFor(inst); s.maps.Has(id) {
			view.Map = id
		}
		views = append(views, view)
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": views})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	if !s.instanceExists(instance) {
		writeError(w, http.StatusNotFound, errInstanceNotFound+instance)
		return
	}

	files, err := s.service.ListFiles(instance)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instance":    instance,
		"profilesDir": s.service.ProfilesDir(instance),
		"files":       files,
	})
}

type analyzeRequest struct {
	Files []string `json:"files"`
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	if !s.instanceExists(instance) {
		writeError(w, http.StatusNotFound, errInstanceNotFound+instance)
		return
	}

	var req analyzeRequest
	if r.Body != nil {
		err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		// An absent body simply means "analyse everything".
		if err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	report, err := s.service.Analyze(instance, req.Files)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	logger.Debug("Analysis served",
		"instance", instance,
		"files", len(report.Files),
		"cached", report.FromCache)

	w.Header().Set(headerCacheControl, "no-store")
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) instanceExists(name string) bool {
	return s.findInstance(name) != nil
}

func (s *Server) findInstance(name string) *config.Instance {
	for i := range s.cfg.Instances {
		if s.cfg.Instances[i].Name == name {
			return &s.cfg.Instances[i]
		}
	}
	return nil
}

// mapImageStems lists the file names an admin may use for a map background,
// e.g. map.png, nasdara.png, nasdara-map.png, map_nasdara.png.
var mapImageStems = []string{"map", "%s", "%s-map", "map_%s"}
var mapImageExts = []string{".jpg", ".jpeg", ".png", ".webp"}

// mapImagePath resolves the background image for an instance.
func (s *Server) mapImagePath(inst *config.Instance) (string, bool) {
	install := s.cfg.GetInstallDir()

	if inst.MapImage != "" {
		p := inst.MapImage
		if !filepath.IsAbs(p) {
			p = filepath.Join(install, p)
		}
		if isRegularFile(p) {
			return p, true
		}
	}

	dirs := []string{
		s.service.ProfilesDir(inst.Name),
		filepath.Join(install, "maps"),
		install,
	}
	if p, ok := firstMapImage(dirs, inst.Name); ok {
		return p, true
	}
	return "", false
}

// firstMapImage looks for map.<ext>, and <instance>.<ext>, in each directory.
func firstMapImage(dirs []string, instance string) (string, bool) {
	for _, dir := range dirs {
		for _, stemPattern := range mapImageStems {
			stem := stemPattern
			if strings.Contains(stemPattern, "%s") {
				stem = fmt.Sprintf(stemPattern, instance)
			}
			for _, ext := range mapImageExts {
				if p := filepath.Join(dir, stem+ext); isRegularFile(p) {
					return p, true
				}
			}
		}
	}
	return "", false
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// remoteMapImage reports whether instances[].map_image points at an http(s)
// URL, which the server proxies so the browser keeps a single endpoint and
// never has to reach a third party itself.
func remoteMapImage(inst *config.Instance) (string, bool) {
	raw := strings.TrimSpace(inst.MapImage)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return u.String(), true
}

func (s *Server) mapImageAvailable(inst *config.Instance) bool {
	if _, ok := remoteMapImage(inst); ok {
		return true
	}
	_, ok := s.mapImagePath(inst)
	return ok
}

func (s *Server) handleMapImage(w http.ResponseWriter, r *http.Request) {
	inst := s.findInstance(r.PathValue("instance"))
	if inst == nil {
		writeError(w, http.StatusNotFound, errInstanceNotFound+r.PathValue("instance"))
		return
	}

	if src, ok := remoteMapImage(inst); ok {
		s.proxyMapImage(w, r, src)
		return
	}

	path, ok := s.mapImagePath(inst)
	if !ok {
		writeError(w, http.StatusNotFound, "no map image for instance "+inst.Name+
			"; put map.jpg in the profiles folder or set instances[].map_image")
		return
	}

	// The image is admin-provided and may be replaced at any time.
	w.Header().Set(headerCacheControl, "no-cache")
	http.ServeFile(w, r, path)
}

// proxyMapImage streams a remote map image through the service.
func (s *Server) proxyMapImage(w http.ResponseWriter, r *http.Request, src string) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "bad map_image URL: "+err.Error())
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cannot fetch map_image: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("map_image returned HTTP %d", resp.StatusCode))
		return
	}

	contentType := resp.Header.Get(headerContentType)
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/jpeg"
	}
	w.Header().Set(headerContentType, contentType)
	w.Header().Set(headerCacheControl, "max-age=300")

	if _, err := io.Copy(w, io.LimitReader(resp.Body, 64<<20)); err != nil {
		logger.Debug("Map image proxy stopped early", "instance", r.PathValue("instance"), "error", err)
	}
}

// ---------------------------------------------------------------------------
// Map tiles
// ---------------------------------------------------------------------------

// mapIDFor resolves the community-map identifier for an instance: the explicit
// `map:` setting wins, otherwise the instance name is used (so an instance
// called "nasdara" needs no configuration at all).
func mapIDFor(inst *config.Instance) string {
	if id := strings.ToLower(strings.TrimSpace(inst.Map)); id != "" {
		return id
	}
	return strings.ToLower(strings.TrimSpace(inst.Name))
}

// handleMaps lists the maps bundled with this binary.
func (s *Server) handleMaps(w http.ResponseWriter, r *http.Request) {
	type summary struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Size    float64 `json:"size"`
		Names   int     `json:"names"`
		MaxZoom int     `json:"maxZoom"`
	}

	maps := make([]summary, 0, 16)
	for _, id := range s.maps.IDs() {
		meta, err := s.maps.Meta(id)
		if err != nil {
			continue
		}
		maps = append(maps, summary{
			ID:      meta.ID,
			Name:    meta.Name,
			Size:    meta.Size,
			Names:   len(meta.Names),
			MaxZoom: meta.MaxZoom,
		})
	}

	w.Header().Set(headerCacheControl, "max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{"maps": maps})
}

// handleMapMeta returns the map's extent, tile limits and place names.
func (s *Server) handleMapMeta(w http.ResponseWriter, r *http.Request) {
	meta, err := s.maps.Meta(r.PathValue("map"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// The data is embedded in the binary, so it only changes with an upgrade.
	w.Header().Set(headerCacheControl, "max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          meta.ID,
		"name":        meta.Name,
		"size":        meta.Size,
		"minZoom":     meta.MinZoom,
		"maxZoom":     meta.MaxZoom,
		"tileSize":    meta.TileSize,
		"source":      meta.Source,
		"version":     meta.Version,
		"names":       meta.Names,
		"tileUrl":     "/api/maps/" + url.PathEscape(meta.ID) + "/tiles/{z}/{x}/{y}",
		"attribution": meta.Attribution,
	})
}

// handleMapTile serves one pyramid tile from the embedded map data.
func (s *Server) handleMapTile(w http.ResponseWriter, r *http.Request) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(r.PathValue("y"))
	if errZ != nil || errX != nil || errY != nil {
		writeError(w, http.StatusBadRequest, "tile coordinates must be integers")
		return
	}

	data, contentType, err := s.maps.Tile(r.PathValue("map"), z, x, y)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set(headerContentType, contentType)
	w.Header().Set(headerCacheControl, "public, max-age=604800, immutable")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set(headerContentType, "application/json; charset=utf-8")
	w.Header().Set(headerCacheControl, "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// noCacheHTML stops browsers from serving a stale index.html after an upgrade.
func noCacheHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The UI files are embedded in the binary, so a new build must not be
		// shadowed by a cached copy of the previous one.
		switch {
		case strings.HasSuffix(r.URL.Path, "/"),
			strings.HasSuffix(r.URL.Path, ".html"),
			strings.HasSuffix(r.URL.Path, ".js"),
			strings.HasSuffix(r.URL.Path, ".css"):
			w.Header().Set(headerCacheControl, "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// withGzip compresses responses; a full analysis can be several megabytes.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	io.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.Writer.Write(b) }

func (g *gzipResponseWriter) WriteHeader(status int) {
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(status)
}

// withLogging records request outcomes at debug level and failures at error.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		entry := logger.Debug
		if rec.status >= http.StatusInternalServerError {
			entry = logger.Error
		}
		entry("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(started).String())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) { return s.ResponseWriter.Write(b) }
