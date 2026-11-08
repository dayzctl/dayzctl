package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Validate checks if the configuration is valid
func (c *ServerConfig) Validate() error {
	if err := requireSteamUser(c); err != nil {
		return err
	}
	if err := requireBasePath(c); err != nil {
		return err
	}
	if err := requireInstances(c); err != nil {
		return err
	}

	for _, inst := range c.Instances {
		if err := validateInstance(inst); err != nil {
			return err
		}
	}

	if err := validateWeb(c); err != nil {
		return err
	}

	return nil
}

func requireSteamUser(c *ServerConfig) error {
	if c.Steam.Username == "" {
		return fmt.Errorf("steam.username is required")
	}
	return nil
}

func requireBasePath(c *ServerConfig) error {
	if c.Paths.Base == "" {
		return fmt.Errorf("paths.base is required")
	}
	return nil
}

func requireInstances(c *ServerConfig) error {
	if len(c.Instances) == 0 {
		return fmt.Errorf("at least one instance must be configured")
	}
	return nil
}

func validateInstance(inst Instance) error {
	if inst.Name == "" {
		return fmt.Errorf("instance name is required")
	}
	if inst.Port == 0 {
		return fmt.Errorf("instance %s: port is required", inst.Name)
	}
	if inst.Enabled && inst.RCON.Enabled {
		if inst.RCON.Port == 0 {
			return fmt.Errorf("instance %s: RCON port is required when RCON is enabled", inst.Name)
		}
		if inst.RCON.Password == "" {
			return fmt.Errorf("instance %s: RCON password is required when RCON is enabled", inst.Name)
		}
	}
	return nil
}

// validateWeb checks the optional web UI settings.
func validateWeb(c *ServerConfig) error {
	if !c.Web.Enabled {
		return nil
	}
	if c.Web.Port < 1 || c.Web.Port > 65535 {
		return fmt.Errorf("web.port must be between 1 and 65535")
	}
	if c.Web.Address == "" {
		return fmt.Errorf("web.address is required when web is enabled")
	}
	if err := validateWebAuth(c.Web.Auth); err != nil {
		return err
	}
	return nil
}

// steamIDPattern matches a SteamID64: 17 digits, all real accounts start 7656.
var steamIDPattern = regexp.MustCompile(`^7656[0-9]{13}$`)

// validateWebAuth refuses a login switch that cannot work: enabled without any
// way to sign in, an account without a secret, a duplicate username, or a Steam
// block that would let everyone in.
func validateWebAuth(a WebAuth) error {
	if !a.Enabled {
		return nil
	}

	accounts := a.Accounts()
	if len(accounts) == 0 && !a.Steam.Enabled {
		return fmt.Errorf("web.auth needs at least one local user or web.auth.steam.enabled")
	}

	seen := make(map[string]bool, len(accounts))
	for i, u := range accounts {
		if strings.TrimSpace(u.Username) == "" {
			return fmt.Errorf("web.auth username is required for each account (users[%d])", i)
		}
		if u.Password == "" && u.PasswordHash == "" {
			return fmt.Errorf("web.auth user %q needs a password or a password_hash", u.Username)
		}
		if u.Password != "" && u.PasswordHash != "" {
			return fmt.Errorf("web.auth user %q: set either password or password_hash, not both", u.Username)
		}
		if u.PasswordHash != "" && len(normalizePasswordHash(u.PasswordHash)) != 64 {
			return fmt.Errorf("web.auth.password_hash must be a SHA-256 hex digest (64 characters)")
		}
		key := strings.ToLower(strings.TrimSpace(u.Username))
		if seen[key] {
			return fmt.Errorf("web.auth username %q is configured twice", u.Username)
		}
		seen[key] = true
	}

	if !a.Steam.Enabled {
		return nil
	}
	if len(a.Steam.AllowedIDs) == 0 {
		return fmt.Errorf("web.auth.steam.allowed_ids is required when steam is enabled")
	}
	for _, id := range a.Steam.AllowedIDs {
		if !steamIDPattern.MatchString(strings.TrimSpace(id)) {
			return fmt.Errorf("web.auth.steam.allowed_ids: %q is not a SteamID64 (17 digits starting 7656)", id)
		}
	}
	if pu := strings.TrimSpace(a.Steam.PublicURL); pu != "" {
		u, err := url.Parse(pu)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("web.auth.steam.public_url must be an absolute http(s) URL")
		}
	}
	return nil
}
