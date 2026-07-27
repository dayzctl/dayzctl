Manage server and client mods for a DayZ instance. Use `all` to target all enabled instances.

Usage:
  dayzctl mods <instance> <command>

Commands:
  list                    List all installed mods for the instance
  add <id> [id...]        Add client mods to the instance
  add-server <id> [id...] Add server-side mods to the instance
  remove <id> [id...]     Remove mods from the instance (config only)
  delete <id> [id...]     Alias for remove
  sync                    Sync mods for the instance

Examples:
  dayzctl mods myinstance list
  dayzctl mods myinstance add 123456
