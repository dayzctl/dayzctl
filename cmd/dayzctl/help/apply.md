Generate server and BattlEye config files and (re)generate systemd unit files.

By default applies to all enabled instances; pass an instance name to target a single instance.

If an instance has 'shutdown_messages' configured, its mission's
db/messages.xml is also (re)generated. DayZ reads this file to broadcast
countdown warnings to players and, for the message marked with
'shutdown: true', to perform its own graceful shutdown (save state, then
exit) once the deadline is reached - this is the server's built-in
"gentle" shutdown mechanism. See 'shutdown_messages' in the config file
for details.

Usage:
  dayzctl apply
  dayzctl apply <instance>

Examples:
  dayzctl apply
  dayzctl apply myserver
