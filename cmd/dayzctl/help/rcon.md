Use RCON to query players and send commands to a running instance. Use 'all' to target all enabled instances.

Usage:
  dayzctl rcon <instance> <command>

Commands:
  players                List online players for the instance
  send <cmd>             Send raw RCON command to instance
  kick <id|guid|ip|name> [reason]  Kick a player by id, GUID, IP or name
  ban <id|guid|ip|name> <minutes>  Ban a player by id, GUID, IP or name
  unban <id|guid|ip>     Remove a ban by ban id, player GUID or IP
  say <message>          Send chat message to instance
  shutdown               Request a graceful server shutdown (saves state, then exits)

Examples:
  dayzctl rcon myserver players
  dayzctl rcon myserver send "status"
  dayzctl rcon myserver kick "Some Player" afk
  dayzctl rcon myserver ban 76561198000000001 60 cheating
  dayzctl rcon myserver unban 0123456789abcdef0123456789abcdef
  dayzctl rcon myserver shutdown
