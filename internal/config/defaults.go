package config

// SetDefaults sets default values for the configuration
func (c *ServerConfig) SetDefaults() {
	if c.Paths.Base == "" {
		c.Paths.Base = "/srv/dayz"
	}

	if c.Server.MaxPlayers == 0 {
		c.Server.MaxPlayers = 60
	}
	if c.Server.SteamQueryPort == 0 {
		c.Server.SteamQueryPort = 27016
	}
	if c.Server.SteamPort == 0 {
		c.Server.SteamPort = 2304
	}
	if c.Server.ClientPort == 0 {
		c.Server.ClientPort = 2304
	}
	if c.Server.VerifySignatures == 0 {
		c.Server.VerifySignatures = 2
	}
	if c.Server.ForceSameBuild == 0 {
		c.Server.ForceSameBuild = 1
	}
	if c.Server.BattlEye == 0 {
		c.Server.BattlEye = 1
	}
	if c.Server.VonCodecQuality == 0 {
		c.Server.VonCodecQuality = 20
	}
	if c.Server.ServerTime == "" {
		c.Server.ServerTime = "SystemTime"
	}
	if c.Server.ServerTimePersistent == 0 {
		c.Server.ServerTimePersistent = 1
	}
	if c.Server.ServerTimeAcceleration == 0 {
		c.Server.ServerTimeAcceleration = 12
	}
	if c.Server.ServerNightTimeAcceleration == 0 {
		c.Server.ServerNightTimeAcceleration = 1
	}
	if c.Server.LoginQueueConcurrentPlayers == 0 {
		c.Server.LoginQueueConcurrentPlayers = 5
	}
	if c.Server.LoginQueueMaxPlayers == 0 {
		c.Server.LoginQueueMaxPlayers = 500
	}
	if c.Server.GuaranteedUpdates == 0 {
		c.Server.GuaranteedUpdates = 1
	}
	if c.Server.NetworkRangeClose == 0 {
		c.Server.NetworkRangeClose = 20
	}
	if c.Server.NetworkRangeNear == 0 {
		c.Server.NetworkRangeNear = 150
	}
	if c.Server.NetworkRangeFar == 0 {
		c.Server.NetworkRangeFar = 1000
	}
	if c.Server.NetworkRangeDistantEffect == 0 {
		c.Server.NetworkRangeDistantEffect = 4000
	}
	if c.Server.SimulatedPlayersBatch == 0 {
		c.Server.SimulatedPlayersBatch = 20
	}
	if c.Server.MultithreadedReplication == 0 {
		c.Server.MultithreadedReplication = 1
	}
	if c.Server.PingWarning == 0 {
		c.Server.PingWarning = 200
	}
	if c.Server.PingCritical == 0 {
		c.Server.PingCritical = 250
	}
	if c.Server.MaxPing == 0 {
		c.Server.MaxPing = 300
	}
	if c.Server.ServerFpsWarning == 0 {
		c.Server.ServerFpsWarning = 15
	}
	if c.Server.StorageAutoFix == 0 {
		c.Server.StorageAutoFix = 1
	}
	if c.Server.LootHistory == 0 {
		c.Server.LootHistory = 1
	}
	if c.Server.RespawnTime == 0 {
		c.Server.RespawnTime = 5
	}
	if c.Server.SpeedhackDetection == 0 {
		c.Server.SpeedhackDetection = 1
	}
	if c.Server.TimeStampFormat == "" {
		c.Server.TimeStampFormat = "Short"
	}
	if c.Server.LogAverageFps == 0 {
		c.Server.LogAverageFps = 1
	}
	if c.Server.LogMemory == 0 {
		c.Server.LogMemory = 1
	}
	if c.Server.LogPlayers == 0 {
		c.Server.LogPlayers = 1
	}
	if c.Server.DefaultVisibility == 0 {
		c.Server.DefaultVisibility = 1375
	}
	if c.Server.DefaultObjectViewDistance == 0 {
		c.Server.DefaultObjectViewDistance = 1375
	}
	if c.Server.ShotValidation == 0 {
		c.Server.ShotValidation = 1
	}
}
