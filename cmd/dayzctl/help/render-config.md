Render the server config to show where files would be written without modifying disk. Useful for validation and debugging templates.

If 'shutdown_messages' is configured, the path of every mission
db/messages.xml that would be written is also shown (one per map in use).

Usage:
  dayzctl render-config <instance>

Examples:
  dayzctl render-config myserver
