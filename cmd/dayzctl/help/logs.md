Show server log output from journald for one or more instances.

Usage:
  dayzctl logs [options] [instance|all]

Running `dayzctl logs` with no arguments prints this help. Pass an instance
name, or "all" to show the logs of every enabled instance. Each instance
maps to its systemd unit dayz@<name>.service.

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
  dayzctl logs -n all all
  dayzctl logs --since "1 hour ago" -f all
