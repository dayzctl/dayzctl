package config

import "fmt"

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
