Generate server and BattlEye config files and (re)generate systemd unit files.

By default applies to all enabled instances; pass an instance name to target a single instance.

If the top-level 'messages' block is configured, the mission
db/messages.xml is (re)generated for every map in use. DayZ reads this
file from the mission folder, not per instance, so it is written once per
mission template:

  <installDir>/mpmissions/<template>/db/messages.xml

Instances sharing a template share the file. The block has one section per
kind of DayZ message, every time value is in minutes:

  welcome:   sent once to every player that connects ('delay' = minutes to
             wait after the connection)
  cyclic:    broadcasts to everyone, 'every' = minutes between them;
             'on_connect: true' also sends that message on connect
             (optionally after 'delay' minutes)
  shutdown:  restart countdown; DayZ announces it at 90, 60, 45, 30, 20,
             15, 10, 5, 2 and 1 minutes before 'deadline', and
             'stop_server: true' saves state and exits when it is reached

Placeholders: #name (server name), #port (server port), #tmin (minutes
remaining, countdown only). Missions with nothing configured keep their
existing messages.xml.

The old flat 'shutdown_messages' list is still accepted and translated
into this block (with a deprecation warning); a block nested in an
instance works as a per-mission override, also deprecated.

Usage:
  dayzctl apply
  dayzctl apply <instance>

Examples:
  dayzctl apply
  dayzctl apply myserver
