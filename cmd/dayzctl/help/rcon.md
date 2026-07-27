Use RCON to query players and send commands to a running instance. Use 'all' to target all enabled instances.

Usage:
  dayzctl rcon <instance> <command>

Commands:
  players                List online players for the instance
  send <cmd>             Send raw RCON command to instance
  kick <id> [reason]     Kick a player by id
  ban <id> <minutes>     Ban a player by id for minutes
  say <message>          Send chat message to instance

Examples:
  dayzctl rcon myserver players
  dayzctl rcon myserver send "status"
