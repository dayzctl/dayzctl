Stop one or more DayZ instance systemd units.

If RCON is enabled for an instance and it is running, a graceful shutdown
is requested first via the RCON "#shutdown" command, letting the DayZ
server save world/player state before exiting. dayzctl waits (default 60s)
for the process to stop on its own; if it doesn't, or RCON is unavailable,
a hard 'systemctl stop' is used instead.

For pre-warned, scheduled restarts (e.g. broadcasting a 5-minute countdown
before the server restarts on its own), configure 'shutdown_messages' on
the instance instead - see 'dayzctl help apply'.

Usage:
  dayzctl stop <instance> [flags]
  dayzctl stop all [flags]

Flags:
  --force, -f        Skip the graceful RCON shutdown and stop immediately
  --timeout <secs>   Seconds to wait for a graceful shutdown before forcing a stop (default 60)

Examples:
  dayzctl stop myserver
  dayzctl stop all
  dayzctl stop myserver --force
  dayzctl stop myserver --timeout 120
