Generate server and BattlEye config files and (re)generate systemd unit files.

By default applies to all enabled instances; pass an instance name to target a single instance.

If 'shutdown_messages' is configured at the top level of the config, the
mission db/messages.xml is (re)generated for every map in use. DayZ reads
this file from the mission folder, not per instance, so it is written once
per mission template:

  <installDir>/mpmissions/<template>/db/messages.xml

Every value is in minutes, and a message can be any mix of the three DayZ
message kinds:

  welcome   - 'on_connect: true' (+ optional 'delay'): sent once to every
              player that connects
  repeating - 'repeat: <minutes>': broadcast to everyone at that interval
  countdown - 'deadline: <minutes>': DayZ announces it at 90, 60, 45, 30, 20,
              15, 10, 5, 2 and 1 minutes before the deadline, and, when
              'shutdown: true', the server saves state and exits once it is
              reached - the built-in "gentle" shutdown. Use '#tmin' in the
              text to show the minutes remaining; the Shutdown flag is
              ignored without 'deadline'.

Missions with nothing configured keep their existing messages.xml.

A 'shutdown_messages' block nested in an instance still works as a
per-mission override, but it is deprecated: instances sharing a mission
share the same file. See 'shutdown_messages' in the config file.

Usage:
  dayzctl apply
  dayzctl apply <instance>

Examples:
  dayzctl apply
  dayzctl apply myserver
