Generate server and BattlEye config files and (re)generate systemd unit files.

By default applies to all enabled instances; pass an instance name to target a single instance.

If an instance has 'shutdown_messages' configured, its mission's
db/messages.xml is also (re)generated. DayZ reads this file to broadcast
countdown warnings to players and, for the message marked with
'shutdown: true', to perform its own graceful shutdown (save state, then
exit) once the deadline is reached - this is the server's built-in
"gentle" shutdown mechanism. Instances without 'shutdown_messages' are
skipped, so an existing messages.xml is left untouched.

Every value is in minutes. A message with 'deadline' is a countdown that
DayZ announces on its own at 90, 60, 45, 30, 20, 15, 10, 5, 2 and 1
minutes before the deadline; use '#tmin' in the text to show the number
of minutes remaining. 'shutdown: true' is ignored without 'deadline'.
See 'shutdown_messages' in the config file for details.

Usage:
  dayzctl apply
  dayzctl apply <instance>

Examples:
  dayzctl apply
  dayzctl apply myserver
