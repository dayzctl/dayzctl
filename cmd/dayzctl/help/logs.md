Show server log output from journald for one or more instances.

Usage:
  dayzctl logs [options] [instance|all]

If no instance is given (or "all" is used), the logs for every enabled
instance are shown. Each instance maps to its systemd unit dayz@<name>.service.

Options:
  -f, --follow        Keep streaming new log entries (like tail -f)
  -n N, -N            Show the last N entries (default 100; use "all" for everything)
  -200f               Tail-style combination: last 200 entries and follow
  --since <when>      Only show entries since <when> (e.g. "today", "2 hours ago")
  --until <when>      Only show entries until <when>
  -h, --help          Show this help

Examples:
  dayzctl logs nasdara
  dayzctl logs nasdara -f
  dayzctl logs nasdara -200f
  dayzctl logs -n all nasdara
  dayzctl logs --since "1 hour ago" -f all
