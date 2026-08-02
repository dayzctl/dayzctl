Restart systemd units for one or more instances.

Equivalent to stop then start; use 'all' to restart every enabled instance.
The stop phase uses the same graceful RCON shutdown behavior as 'dayzctl
stop' (see 'dayzctl help stop'), giving the server a chance to save state
before it is restarted.

Usage:
  dayzctl restart <instance> [flags]
  dayzctl restart all [flags]

Flags:
  --force, -f        Skip the graceful RCON shutdown and stop immediately
  --timeout <secs>   Seconds to wait for a graceful shutdown before forcing a stop (default 60)

Examples:
  dayzctl restart myserver
  dayzctl restart all
  dayzctl restart myserver --force
