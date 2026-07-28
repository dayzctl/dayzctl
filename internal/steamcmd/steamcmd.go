package steamcmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dayzctl/dayzctl/internal/logger"
	"github.com/dayzctl/dayzctl/internal/utils"
)

// Common errors
var (
	ErrRateLimited = errors.New("steam rate limited")
	ErrLoginFailed = errors.New("steam login failed")
)

// SteamCmd represents a SteamCMD instance
type SteamCmd struct {
	User         string
	InstallDir   string
	SteamCmdPath string
	WorkshopDir  string
	lastAttempt  time.Time
	mu           sync.Mutex
}

// New creates a new SteamCMD instance. If workshopDir is empty, it falls
// back to <installDir>/workshop.
func New(user, installDir, steamCmdPath, workshopDir string) *SteamCmd {
	workshop := workshopDir
	if workshop == "" {
		workshop = filepath.Join(installDir, "workshop")
	}
	return &SteamCmd{
		User:         user,
		InstallDir:   installDir,
		SteamCmdPath: steamCmdPath,
		WorkshopDir:  workshop,
	}
}

// runSteamCmd runs a steamcmd command as the dayz user
func (s *SteamCmd) runSteamCmd(args ...string) error {
	if s.SteamCmdPath == "" {
		return fmt.Errorf("steamcmd binary path not configured")
	}
	cmdStr := fmt.Sprintf("%s %s", s.SteamCmdPath, strings.Join(args, " "))
	logger.Debug("Executing steamcmd", "cmd", cmdStr, "user", s.User, "installDir", s.InstallDir)

	cmd := exec.Command("runuser", "-u", "dayz", "--", "sh", "-c", cmdStr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logger.Warn("steamcmd run failed", "cmd", cmdStr, "error", err)
		return err
	}
	return nil
}

// runSteamCmdWithOutput runs a steamcmd command and returns output
func (s *SteamCmd) runSteamCmdWithOutput(args ...string) (string, error) {
	if s.SteamCmdPath == "" {
		return "", fmt.Errorf("steamcmd binary path not configured")
	}
	cmdStr := fmt.Sprintf("%s %s", s.SteamCmdPath, strings.Join(args, " "))
	logger.Debug("Executing steamcmd (with output)", "cmd", cmdStr, "user", s.User, "installDir", s.InstallDir)

	cmd := exec.Command("runuser", "-u", "dayz", "--", "sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		logger.Warn("steamcmd returned error", "cmd", cmdStr, "error", err, "output", outStr)
	} else {
		logger.Debug("steamcmd output", "cmd", cmdStr)
	}
	return outStr, err
}

// GetBuildID retrieves the current build ID from Steam
func (s *SteamCmd) GetBuildID() (string, error) {
	if cached := s.getCachedBuildID(); cached != "" {
		logger.Debug("Using cached build ID", "build_id", cached)
		return cached, nil
	}

	logger.Info("Fetching build ID from Steam...")
	output, err := s.runSteamCmdWithOutput(
		"+@sSteamCmdForcePlatformType", "linux",
		"+login", "anonymous",
		"+app_info_print", "223350",
		"+quit",
	)
	if err != nil {
		if strings.Contains(output, "Rate Limit Exceeded") {
			return "", ErrRateLimited
		}
		return "", fmt.Errorf("steamcmd failed: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"buildid"`) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				buildID := strings.Trim(parts[1], `"`)
				s.cacheBuildID(buildID)
				logger.Info("Retrieved build ID", "build_id", buildID)
				return buildID, nil
			}
		}
	}
	return "", fmt.Errorf("buildid not found in output")
}

// getCurrentLocalBuildID reads the current local build ID
func (s *SteamCmd) getCurrentLocalBuildID() (string, error) {
	appManifestPath := filepath.Join(s.InstallDir, "steamapps", "appmanifest_223350.acf")
	logger.Debug("Checking local build ID", "path", appManifestPath)

	if _, err := os.Stat(appManifestPath); err == nil {
		data, err := os.ReadFile(appManifestPath)
		if err == nil {
			content := string(data)
			re := regexp.MustCompile(`"buildid"\s+"(\d+)"`)
			matches := re.FindStringSubmatch(content)
			if len(matches) > 1 {
				buildID := matches[1]
				logger.Info("Local build ID from manifest", "build_id", buildID)
				return buildID, nil
			}
		}
	} else {
		logger.Warn("App manifest not found, server not installed", "path", appManifestPath)
		// Clear the cache since server is not installed
		s.clearCache()
		return "", nil
	}

	return "", nil
}

// clearCache removes the cached build ID
func (s *SteamCmd) clearCache() {
	cacheFile := "/tmp/steam_buildid_" + s.User
	if err := os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
		logger.Warn("Failed to clear cache", "error", err)
	} else {
		logger.Debug("Cleared build ID cache", "file", cacheFile)
	}
}

// NeedsUpdate checks if the server needs an update
func (s *SteamCmd) NeedsUpdate() (bool, error) {
	logger.Info("Checking for updates...")

	latestBuildID, err := s.GetBuildID()
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			return false, err
		}
		return false, err
	}

	localBuildID, err := s.getCurrentLocalBuildID()
	if err != nil || localBuildID == "" {
		logger.Warn("Could not determine local build ID, assuming update needed")
		return true, nil
	}

	logger.Debug("Comparing builds", "local", localBuildID, "latest", latestBuildID)
	needsUpdate := localBuildID != latestBuildID
	logger.Info("Update status", "needs_update", needsUpdate, "local_build", localBuildID, "latest_build", latestBuildID)
	return needsUpdate, nil
}

// Update updates the DayZ server
func (s *SteamCmd) Update() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRateLimited() {
		return ErrRateLimited
	}

	logger.Info("Starting server update...")
	err := s.runSteamCmd(
		"+@sSteamCmdForcePlatformType", "linux",
		"+force_install_dir", s.InstallDir,
		"+login", s.User,
		"+app_update", "223350", "validate",
		"+quit",
	)

	s.lastAttempt = time.Now()
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	logger.Info("Server update completed successfully")
	return nil
}

// DownloadMod downloads a mod using anonymous login
func (s *SteamCmd) DownloadMod(modID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRateLimited() {
		return ErrRateLimited
	}

	logger.Info("Downloading mod", "mod_id", modID)
	logger.Debug("Mod download settings", "workshopDir", s.WorkshopDir, "installDir", s.InstallDir)

	if err := os.MkdirAll(s.WorkshopDir, 0755); err != nil {
		return fmt.Errorf("failed to create workshop directory: %w", err)
	}

	// Use configured user if present; fall back to anonymous otherwise.
	loginUser := "anonymous"
	if s.User != "" {
		loginUser = s.User
	}

	output, err := s.runSteamCmdWithOutput(
		"+@sSteamCmdForcePlatformType", "linux",
		"+force_install_dir", s.WorkshopDir,
		"+login", loginUser,
		"+workshop_download_item", "221100", modID, "validate",
		"+quit",
	)

	s.lastAttempt = time.Now()
	if err != nil {
		if strings.Contains(output, "Rate Limit Exceeded") {
			return ErrRateLimited
		}
		return fmt.Errorf("mod download failed: %w\nOutput: %s", err, output)
	}

	if strings.Contains(output, "Success.") || strings.Contains(output, "already downloaded") {
		logger.Info("Mod downloaded successfully", "mod_id", modID)
		return s.linkMod(modID)
	}

	return fmt.Errorf("mod %s download failed - check output", modID)
}

// linkMod creates the symlink for a mod in the workshop directory
func (s *SteamCmd) linkMod(modID string) error {
	possiblePaths := []string{
		filepath.Join(s.WorkshopDir, "steamapps", "workshop", "content", "221100", modID),
		filepath.Join(s.WorkshopDir, "content", "221100", modID),
		filepath.Join(s.WorkshopDir, modID),
		filepath.Join("/home/dayz/Steam/steamapps/workshop/content/221100", modID),
		filepath.Join("/home/dayz/.steam/steamapps/workshop/content/221100", modID),
		filepath.Join(s.InstallDir, "steamapps", "workshop", "content", "221100", modID),
	}

	var srcPath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			srcPath = path
			logger.Debug("Found mod at", "path", path)
			break
		}
	}

	if srcPath == "" {
		return fmt.Errorf("mod %s not found in any workshop location", modID)
	}

	targetPath := filepath.Join(s.WorkshopDir, modID)

	if _, err := os.Lstat(targetPath); err == nil {
		if err := os.Remove(targetPath); err != nil {
			return fmt.Errorf("failed to remove existing symlink: %w", err)
		}
	}

	if err := os.Symlink(srcPath, targetPath); err != nil {
		return fmt.Errorf("failed to create symlink from %s to %s: %w", targetPath, srcPath, err)
	}

	// Use ChownSymlink to change only the symlink itself, not the target
	if err := utils.ChownSymlink(targetPath); err != nil {
		logger.Warn("Failed to chown workshop symlink", "path", targetPath, "error", err)
	}

	logger.Info("Mod linked successfully", "mod_id", modID, "path", targetPath)
	return nil
}

// InteractiveLogin performs an interactive Steam login
func (s *SteamCmd) InteractiveLogin() error {
	cmdStr := fmt.Sprintf("%s +login %s +quit", s.SteamCmdPath, s.User)
	logger.Info("Starting interactive Steam login", "cmd", cmdStr, "user", s.User)

	cmd := exec.Command(
		"runuser", "-u", "dayz", "--",
		"sh", "-c", cmdStr,
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		logger.Warn("interactive login failed", "cmd", cmdStr, "error", err)
		return fmt.Errorf("interactive login failed: %w", err)
	}
	return nil
}

// isRateLimited checks if we're rate limited
func (s *SteamCmd) isRateLimited() bool {
	return time.Since(s.lastAttempt) < 5*time.Minute
}

// getCachedBuildID retrieves cached build ID
func (s *SteamCmd) getCachedBuildID() string {
	cacheFile := "/tmp/steam_buildid_" + s.User
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// cacheBuildID caches the build ID
func (s *SteamCmd) cacheBuildID(buildID string) {
	cacheFile := "/tmp/steam_buildid_" + s.User
	if err := os.WriteFile(cacheFile, []byte(buildID), 0644); err != nil {
		_ = err
	}
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	return errors.Is(err, ErrRateLimited)
}
