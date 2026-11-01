package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
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
	return validateShutdownMessages(inst)
}

// validateShutdownMessages rejects message blocks DayZ would ignore, so the
// problem surfaces at config load instead of as messages that never appear
// in game. See "DayZ: Server Messages" for the supported format.
func validateShutdownMessages(inst Instance) error {
	for i, m := range inst.ShutdownMessages {
		where := fmt.Sprintf("instance %s: shutdown_messages[%d]", inst.Name, i)
		if strings.TrimSpace(m.Text) == "" {
			return fmt.Errorf("%s: text is required", where)
		}
		if utf8.RuneCountInString(m.Text) > 160 {
			return fmt.Errorf("%s: text is longer than DayZ's 160 character limit", where)
		}
		if m.Deadline < 0 || m.Repeat < 0 {
			return fmt.Errorf("%s: deadline and repeat are minutes and must not be negative", where)
		}
		if m.Shutdown && m.Deadline == 0 {
			return fmt.Errorf("%s: shutdown requires deadline > 0 (DayZ ignores it for non-countdown messages)", where)
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

// validateWebAuth refuses a login switch that cannot work: enabled without a
// username, or without either a password or a password hash.
func validateWebAuth(a WebAuth) error {
	if !a.Enabled {
		return nil
	}
	if a.Username == "" {
		return fmt.Errorf("web.auth.username is required when web.auth.enabled is true")
	}
	if a.Password == "" && a.PasswordHash == "" {
		return fmt.Errorf("web.auth needs a password or a password_hash when enabled")
	}
	if a.Password != "" && a.PasswordHash != "" {
		return fmt.Errorf("set either web.auth.password or web.auth.password_hash, not both")
	}
	if h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a.PasswordHash), "sha256:")); a.PasswordHash != "" && len(h) != 64 {
		return fmt.Errorf("web.auth.password_hash must be a SHA-256 hex digest (64 characters)")
	}
	return nil
}
