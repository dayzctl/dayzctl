Run the web UI and its log analysis API.

Usage:
  dayzctl web [options]

The web UI reads the DayZ logs from every configured instance and serves them
as an analyser: players, kills, base building, suspicious activity, a timeline
and a map. Parsing happens on the server; the browser only renders the result.
Nothing is ever written to the DayZ install.

Logs come from the profiles folder the instance unit already uses:
  <install_dir>/profiles/<instance>/script_*.ADM

Options:
  -a, --addr <address>  Listen address (default: web.address, else 127.0.0.1)
  -p, --port <port>     Listen port (default: web.port, else 8081)
  -h, --help            Show this help

Configuration (config.yaml):
  web:
    enabled: true          # install and enable dayz-web.service on apply
    address: 127.0.0.1     # 0.0.0.0 is safe once web.auth is on
    port: 8081
    auth:
      enabled: true        # require a sign-in for the UI and /api/
      session_hours: 12
      steam:               # optional Steam SSO (OpenID 2.0)
        enabled: true
        admins:            # SteamID64 -> display name (the name is optional)
          "76561198000000000": "Alice"

  server:
    password_admin: change-me   # the web login password (also in-game/RCON)

Endpoints:
  GET  /login                              sign-in page
  POST /login                              admin-password sign-in
  GET  /auth/steam                         start Steam SSO
  GET  /auth/steam/callback                Steam return (verified server-side)
  POST /logout                             end the session
  GET  /api/health
  GET  /api/instances
  GET  /api/instances/<instance>/logs
  POST /api/instances/<instance>/analyze   {"files":["script_x.ADM"]}
                                           omit "files" to analyse all

Examples:
  dayzctl web
  dayzctl web --port 9000
  dayzctl web --addr 0.0.0.0 --port 8081

systemd:
  `dayzctl apply` is the only thing that installs services. It writes
  /etc/systemd/system/dayz-web.service when web.enabled is true, enables it,
  and removes and disables it when web.enabled is false. A unit file is only
  rewritten when its content changed, and `daemon-reload` only runs when
  something did. Control the service with:
    systemctl start|stop|status dayz-web.service
    journalctl -u dayz-web.service -f

With web.auth on, every request needs a session cookie. The password form
checks server.password_admin (the same admin password the game and RCON use);
Steam users sign in through
steamcommunity.com and are only accepted when their SteamID64 is listed in
web.auth.steam.admins. Sessions live in memory, so restarting the service
signs everyone out. Keep the loopback address, or terminate TLS in a reverse
proxy such as nginx or Caddy.
