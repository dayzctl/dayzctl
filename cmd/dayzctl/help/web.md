Run the web UI and its log analysis API.

Usage:
  dayzctl web [options]

The web UI reads the DayZ logs from every configured instance and serves them
as an analyser: RPT errors explained in plain words, ADM players, kills, base
building, suspicious activity, a timeline and a map. Parsing happens on the
server; the browser only renders the result. Nothing is ever written to the
DayZ install.

Logs come from the profiles folder the instance unit already uses:
  <install_dir>/profiles/<instance>/script_*.ADM
  <install_dir>/profiles/<instance>/script_*.RPT

Options:
  -a, --addr <address>  Listen address (default: web.address, else 127.0.0.1)
  -p, --port <port>     Listen port (default: web.port, else 8081)
  -h, --help            Show this help

Configuration (config.yaml):
  web:
    enabled: true          # install and enable dayz-web.service on apply
    address: 127.0.0.1     # use 0.0.0.0 only behind an authenticating proxy
    port: 8081

Endpoints:
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

There is no built-in authentication: keep the default loopback address, or
terminate TLS and authentication in a reverse proxy such as nginx or Caddy.
