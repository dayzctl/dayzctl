Run an interactive SteamCMD login flow for the configured Steam user. Required for workshop downloads when credentials are needed.

This command must be run as root; SteamCMD itself is executed as the `dayz`
user. It reads from the terminal, so you can enter your Steam password and,
when Steam Guard is enabled, the mobile authenticator or email code at the
prompts. After a successful login SteamCMD caches the credentials/sentry for
the `dayz` user, so later `dayzctl update` and mod downloads do not prompt
again.

If no terminal is attached (for example, a non-interactive SSH session or a
piped invocation), the command refuses to run instead of sending an empty
password to Steam. Use `ssh -t` or run it from an interactive shell.

Usage:
  dayzctl steam-login

Examples:
  dayzctl steam-login
