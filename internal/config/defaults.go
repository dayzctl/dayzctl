package config

// SetDefaults sets default values for the configuration
func (c *ServerConfig) SetDefaults() {
	// helper setters reduce repetitive checks
	setStrDefault := func(p *string, def string) {
		if p != nil && *p == "" {
			*p = def
		}
	}
	setIntDefault := func(p *int, def int) {
		if p != nil && *p == 0 {
			*p = def
		}
	}

	setFloatDefault := func(p *float64, def float64) {
		if p != nil && *p == 0 {
			*p = def
		}
	}

	setStrDefault(&c.Paths.Base, "/srv/dayz")

	setIntDefault(&c.Server.MaxPlayers, 60)
	setIntDefault(&c.Server.SteamQueryPort, 27016)
	setIntDefault(&c.Server.SteamPort, 2304)
	setIntDefault(&c.Server.ClientPort, 2304)
	setIntDefault(&c.Server.VerifySignatures, 2)
	setIntDefault(&c.Server.ForceSameBuild, 1)
	setIntDefault(&c.Server.BattlEye, 1)
	setIntDefault(&c.Server.VonCodecQuality, 20)
	setStrDefault(&c.Server.ServerTime, "SystemTime")
	setIntDefault(&c.Server.ServerTimePersistent, 1)
	setIntDefault(&c.Server.ServerTimeAcceleration, 12)
	setFloatDefault(&c.Server.ServerNightTimeAcceleration, 1)
	setIntDefault(&c.Server.LoginQueueConcurrentPlayers, 5)
	setIntDefault(&c.Server.LoginQueueMaxPlayers, 500)
	setIntDefault(&c.Server.GuaranteedUpdates, 1)
	setIntDefault(&c.Server.NetworkRangeClose, 20)
	setIntDefault(&c.Server.NetworkRangeNear, 150)
	setIntDefault(&c.Server.NetworkRangeFar, 1000)
	setIntDefault(&c.Server.NetworkRangeDistantEffect, 4000)
	setIntDefault(&c.Server.SimulatedPlayersBatch, 20)
	setIntDefault(&c.Server.MultithreadedReplication, 1)
	setIntDefault(&c.Server.PingWarning, 200)
	setIntDefault(&c.Server.PingCritical, 250)
	setIntDefault(&c.Server.MaxPing, 300)
	setIntDefault(&c.Server.ServerFpsWarning, 15)
	setIntDefault(&c.Server.StorageAutoFix, 1)
	setIntDefault(&c.Server.LootHistory, 1)
	setIntDefault(&c.Server.RespawnTime, 5)
	setIntDefault(&c.Server.SpeedhackDetection, 1)
	setStrDefault(&c.Server.TimeStampFormat, "Short")
	setIntDefault(&c.Server.LogAverageFps, 1)
	setIntDefault(&c.Server.LogMemory, 1)
	setIntDefault(&c.Server.LogPlayers, 1)
	setIntDefault(&c.Server.DefaultVisibility, 1375)
	setIntDefault(&c.Server.DefaultObjectViewDistance, 1375)
	setIntDefault(&c.Server.ShotValidation, 1)
}
