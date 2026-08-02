Render the server config to show where files would be written without modifying disk. Useful for validation and debugging templates.

If the instance has 'shutdown_messages' configured, the path where its
mission's db/messages.xml would be written is also shown.

Usage:
  dayzctl render-config <instance>

Examples:
  dayzctl render-config myserver
