Generate server and BattlEye config files and (re)generate systemd unit files.

By default applies to all enabled instances; pass an instance name to target a single instance.

If 'shutdown_messages' is configured at the top level of the config, the
mission db/messages.xml is (re)generated for every map in use. DayZ reads
this file from the mission folder, not per instance, so it is written once
per mission template:

  <installDir>/mpmissions/<template>/db/messages.xml

Instances sharing a template share the file. Notes:
  - every time value is in minutes
  - a message with 'deadline' is a countdown: DayZ announces it at 90, 60,
    45, 30, 20, 15, 10, 5, 2 and 1 minutes before the deadline; 'shutdown:
    true' saves state and exits when it is reached, and is ignored without
    'deadline'
  - placeholders: #name (server name), #port (server port), #tmin (minutes
    remaining, countdown messages only)

Missions with nothing configured keep their existing messages.xml.

A 'shutdown_messages' block nested in an instance still works as a
per-mission override, but it is deprecated. See 'shutdown_messages' in the
config file.

Usage:
  dayzctl apply
  dayzctl apply <instance>

Examples:
  dayzctl apply
  dayzctl apply myserver
