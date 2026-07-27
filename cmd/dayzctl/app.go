package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands"
	modsCmds "github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/mods"
	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/rcon"
	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/kabroxiko/dayzctl/internal/config"
	"github.com/kabroxiko/dayzctl/internal/logger"
	cli "github.com/urfave/cli/v2"
)

//go:embed help/*.md
var helpFS embed.FS

func NewApp() *cli.App {
	// printCommandExtended attempts to print an extended help block for a
	// named command using the command's Description, Usage, ArgsUsage and
	// subcommands. Returns true when printed.
	printCommandExtended := func(c *cli.Context, name string) bool {
		// Only use embedded help files. Do not fall back to command metadata.
		if b, err := helpFS.ReadFile("help/" + name + ".md"); err == nil {
			_, _ = fmt.Fprintln(os.Stdout, string(b))
			_, _ = fmt.Fprintln(os.Stdout, "")
			return true
		}
		return false
	}

	// showHelpSafe attempts to show extended help for a named command using
	// only embedded help files. It does not fall back to command metadata
	// or cli.ShowCommandHelp.
	showHelpSafe := func(c *cli.Context, name string, _ func()) error {
		if printed := printCommandExtended(c, name); printed {
			return nil
		}
		_, _ = fmt.Fprintf(os.Stdout, "No help available for '%s'\n", name)
		return nil
	}
	app := &cli.App{
		Name:        "dayzctl",
		Usage:       "DayZ server management tool",
		Description: "",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "log-level", Value: "info", Usage: "Log level (debug, info, warn, error)"},
		},
		Before: func(c *cli.Context) error {
			// Prioritize help/version invocations before loading config or enforcing root.
			// If the user invoked no subcommand (just `dayzctl`) or asked for global
			// help/version or provided a single subcommand name (e.g. `dayzctl rcon`),
			// skip root check and config loading so the command's help can be displayed.
			firstArg := ""
			if len(os.Args) > 1 {
				firstArg = os.Args[1]
			}
			if firstArg == "" || firstArg == "-h" || firstArg == "--help" || firstArg == "help" || firstArg == "version" {
				return nil
			}

			// Allow tests and special callers to skip the root check by setting
			// DAYZCTL_SKIP_ROOT=1 in the environment.
			if os.Getenv("DAYZCTL_SKIP_ROOT") != "1" {
				if os.Geteuid() != 0 {
					return cli.Exit("dayzctl must be run as root", 1)
				}
			}
			cfgPath := config.DefaultConfigPath()
			// Test harnesses set DAYZCTL_SKIP_ROOT=1; when present allow a
			// test-only override via DAYZCTL_CONFIG so integration tests can
			// supply a temporary config file without writing to /etc.
			if os.Getenv("DAYZCTL_SKIP_ROOT") == "1" {
				if v := os.Getenv("DAYZCTL_CONFIG"); v != "" {
					cfgPath = v
				}
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return cli.Exit("failed to load config: "+err.Error(), 1)
			}
			shared.Config = cfg

			// Support `--log-level` passed after subcommands by scanning os.Args.
			lvl := c.String("log-level")
			for i, a := range os.Args {
				if strings.HasPrefix(a, "--log-level=") {
					lvl = strings.SplitN(a, "=", 2)[1]
					break
				}
				if a == "--log-level" && i+1 < len(os.Args) {
					lvl = os.Args[i+1]
					break
				}
			}
			logger.Init(lvl)
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:        "rcon",
				Usage:       "RCON commands for an instance",
				Description: "",
				ArgsUsage:   "<instance> <command>",
				Action: func(c *cli.Context) error {
					// Preserve legacy UX: accept `rcon <instance> <command>`
					inst := c.Args().Get(0)
					if inst == "" {
						return showHelpSafe(c, "rcon", nil)
					}
					sub := c.Args().Get(1)
					if sub == "" {
						return showHelpSafe(c, "rcon", nil)
					}

					// Strip the subcommand from the tail so handlers receive only
					// the arguments intended for the subcommand itself.
					tail := c.Args().Tail()
					if len(tail) > 0 {
						tail = tail[1:]
					}
					switch sub {
					case "players":
						return rcon.PlayersAction(inst, tail)
					case "send":
						return rcon.SendAction(inst, tail)
					case "kick":
						return rcon.KickAction(inst, tail)
					case "ban":
						return rcon.BanAction(inst, tail)
					case "say":
						return rcon.SayAction(inst, tail)
					default:
						// Unknown rcon subcommand — show rcon help
						return showHelpSafe(c, "rcon", nil)
					}
				},
			},
			// Core instance and utility commands
			{
				Name:        "list",
				Usage:       "List all configured instances",
				Description: "",
				Action: func(c *cli.Context) error {
					return commands.ListInstancesAction()
				},
			},
			{
				Name:        "start",
				Usage:       "Start a server instance or all instances",
				Description: "",
				ArgsUsage:   "[instance|all]",
				Action: func(c *cli.Context) error {
					target := c.Args().Get(0)
					if target == "" {
						return showHelpSafe(c, "start", nil)
					}
					return commands.StartAction(target)
				},
			},
			{
				Name:        "stop",
				Usage:       "Stop a server instance or all instances",
				Description: "",
				ArgsUsage:   "[instance|all]",
				Action: func(c *cli.Context) error {
					target := c.Args().Get(0)
					if target == "" {
						return showHelpSafe(c, "stop", nil)
					}
					return commands.StopAction(target)
				},
			},
			{
				Name:        "restart",
				Usage:       "Restart a server instance or all instances",
				Description: "",
				ArgsUsage:   "[instance|all]",
				Action: func(c *cli.Context) error {
					target := c.Args().Get(0)
					if target == "" {
						return showHelpSafe(c, "restart", nil)
					}
					return commands.RestartAction(target)
				},
			},
			{
				Name:        "status",
				Usage:       "Show status of server instance(s)",
				Description: "",
				ArgsUsage:   "[instance|all]",
				Action: func(c *cli.Context) error {
					arg := c.Args().Get(0)
					return commands.StatusAction(arg)
				},
			},
			{
				Name:        "apply",
				Usage:       "Apply configuration and generate systemd units",
				Description: "",
				ArgsUsage:   "[instance|all]",
				Action: func(c *cli.Context) error {
					target := c.Args().Get(0)
					return commands.ApplyAction(target)
				},
			},
			{
				Name:        "update",
				Usage:       "Update DayZ server and sync mods",
				Description: "",
				Action: func(c *cli.Context) error {
					return commands.UpdateAction()
				},
			},
			{
				Name:        "validate-config",
				Usage:       "Validate the configuration file",
				Description: "",
				Action: func(c *cli.Context) error {
					return commands.ValidateConfigAction()
				},
			},
			{
				Name:        "render-config",
				Usage:       "Render server config for an instance (dry-run)",
				Description: "",
				ArgsUsage:   "[instance]",
				Action: func(c *cli.Context) error {
					inst := c.Args().Get(0)
					if inst == "" {
						return showHelpSafe(c, "render-config", nil)
					}
					return commands.RenderConfigAction(inst)
				},
			},
			{
				Name:        "check-space",
				Usage:       "Check available disk space for install directory",
				Description: "",
				Action: func(c *cli.Context) error {
					return commands.CheckSpaceAction()
				},
			},
			{
				Name:        "steam-login",
				Usage:       "Interactive Steam login for workshop operations",
				Description: "",
				Action: func(c *cli.Context) error {
					return commands.SteamLoginAction()
				},
			},
			{
				Name:        "version",
				Usage:       "Print version information",
				Description: "",
				Action: func(c *cli.Context) error {
					return commands.VersionAction()
				},
			},
			{
				Name:        "mods",
				Usage:       "Manage mods for an instance",
				Description: "",
				ArgsUsage:   "<instance> <command>",
				Action: func(c *cli.Context) error {
					// Instance-first invocation: `mods <instance> <command>`
					inst := c.Args().Get(0)
					if inst == "" {
						_ = showHelpSafe(c, "mods", nil)
						return nil
					}
					sub := c.Args().Get(1)
					if sub == "" {
						_ = showHelpSafe(c, "mods", nil)
						return nil
					}

					// Strip subcommand from tail so handlers receive only
					// the arguments intended for the subcommand itself.
					tail := c.Args().Tail()
					if len(tail) > 0 {
						tail = tail[1:]
					}
					switch sub {
					case "list":
						return modsCmds.ListAction(inst, tail)
					case "add":
						return modsCmds.AddAction(inst, tail)
					case "add-server":
						return modsCmds.AddServerAction(inst, tail)
					case "remove":
						return modsCmds.RemoveAction(inst, tail)
					case "delete":
						del := c.Bool("delete-files")
						return modsCmds.DeleteAction(inst, tail, del)
					case "sync":
						return modsCmds.SyncAction(inst)
					default:
						return showHelpSafe(c, "mods", nil)
					}
				},
			},
		},
	}
	return app
}

func RunApp() {
	app := NewApp()
	if err := app.Run(os.Args); err != nil {
		log.Fatalf("app error: %v", err)
	}
}
