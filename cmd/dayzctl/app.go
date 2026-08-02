package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands"
	modsCmds "github.com/dayzctl/dayzctl/cmd/dayzctl/commands/mods"
	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/rcon"
	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"

	cli "github.com/urfave/cli/v2"
)

//go:embed help/*.md
var helpFS embed.FS

const (
	ArgInstanceAll = "[instance|all]"
	ArgInstance    = "[instance]"
)

// printCommandExtended attempts to print an extended help block for a
// named command using the command's Description, Usage, ArgsUsage and
// subcommands. Returns true when printed.
func printCommandExtended(name string) bool {
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
func showHelpSafe(c *cli.Context, name string) error {
	if printed := printCommandExtended(name); printed {
		return nil
	}
	_, _ = fmt.Fprintf(os.Stdout, "No help available for '%s'\n", name)
	return nil
}

// rconCommandHandler implements the rcon command action.
func rconCommandHandler(c *cli.Context) error {
	inst := c.Args().Get(0)
	if inst == "" {
		return showHelpSafe(c, "rcon")
	}
	sub := c.Args().Get(1)
	if sub == "" {
		return showHelpSafe(c, "rcon")
	}

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
	case "shutdown":
		return rcon.ShutdownAction(inst, tail)
	default:
		return showHelpSafe(c, "rcon")
	}
}

// modsCommandHandler implements the mods command action.
func modsCommandHandler(c *cli.Context) error {
	inst := c.Args().Get(0)
	if inst == "" {
		_ = showHelpSafe(c, "mods")
		return nil
	}
	sub := c.Args().Get(1)
	if sub == "" {
		_ = showHelpSafe(c, "mods")
		return nil
	}

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
		return showHelpSafe(c, "mods")
	}
}
func NewApp() *cli.App {
	return &cli.App{
		Name:        "dayzctl",
		Usage:       "DayZ server management tool",
		Description: "",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "log-level", Value: "info", Usage: "Log level (debug, info, warn, error)"},
		},
		Before:   setupBefore,
		Commands: buildCommands(),
	}
}

// setupBefore is the extracted Before hook used by the CLI app. It enforces
// root when appropriate, loads configuration, and initializes logging.
func setupBefore(c *cli.Context) error {
	if isHelpInvocation() {
		return nil
	}
	if err := enforceRoot(); err != nil {
		return err
	}

	cfgPath := config.DefaultConfigPath()
	if os.Getenv("DAYZCTL_SKIP_ROOT") == "1" {
		if v := os.Getenv("DAYZCTL_CONFIG"); v != "" {
			cfgPath = v
		}
	}
	if err := loadConfig(cfgPath); err != nil {
		return cli.Exit("failed to load config: "+err.Error(), 1)
	}

	initLogging(c)
	return nil
}

// isHelpInvocation returns true when the CLI was invoked in a way that only
// requests help or version information and does not require full setup.
func isHelpInvocation() bool {
	firstArg := ""
	if len(os.Args) > 1 {
		firstArg = os.Args[1]
	}
	return firstArg == "" || firstArg == "-h" || firstArg == "--help" || firstArg == "help" || firstArg == "version"
}

// enforceRoot verifies we are running as root unless tests opt out.
func enforceRoot() error {
	if os.Getenv("DAYZCTL_SKIP_ROOT") != "1" {
		if os.Geteuid() != 0 {
			return cli.Exit("dayzctl must be run as root", 1)
		}
	}
	return nil
}

// loadConfig loads configuration from the given path and stores it in
// the shared package state.
func loadConfig(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	shared.Config = cfg
	return nil
}

// initLogging initializes the global logger, respecting `--log-level` even
// when passed after the subcommand.
func initLogging(c *cli.Context) {
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
}

// buildCommands returns the list of top-level CLI commands. Extracted to keep
// NewApp concise and easier to reason about.
func buildCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:        "rcon",
			Usage:       "RCON commands for an instance",
			Description: "",
			ArgsUsage:   "<instance> <command>",
			Action:      rconCommandHandler,
		},
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
			ArgsUsage:   ArgInstanceAll,
			Action: func(c *cli.Context) error {
				target := c.Args().Get(0)
				if target == "" {
					return showHelpSafe(c, "start")
				}
				return commands.StartAction(target)
			},
		},
		{
			Name:        "stop",
			Usage:       "Stop a server instance or all instances",
			Description: "",
			ArgsUsage:   ArgInstanceAll,
			Flags: []cli.Flag{
				&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Skip graceful RCON shutdown and stop immediately"},
				&cli.IntFlag{Name: "timeout", Value: 60, Usage: "Seconds to wait for a graceful shutdown before forcing a stop"},
			},
			Action: func(c *cli.Context) error {
				target := c.Args().Get(0)
				if target == "" {
					return showHelpSafe(c, "stop")
				}
				timeout := time.Duration(c.Int("timeout")) * time.Second
				return commands.StopAction(target, c.Bool("force"), timeout)
			},
		},
		{
			Name:        "restart",
			Usage:       "Restart a server instance or all instances",
			Description: "",
			ArgsUsage:   ArgInstanceAll,
			Flags: []cli.Flag{
				&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Skip graceful RCON shutdown and stop immediately"},
				&cli.IntFlag{Name: "timeout", Value: 60, Usage: "Seconds to wait for a graceful shutdown before forcing a stop"},
			},
			Action: func(c *cli.Context) error {
				target := c.Args().Get(0)
				if target == "" {
					return showHelpSafe(c, "restart")
				}
				timeout := time.Duration(c.Int("timeout")) * time.Second
				return commands.RestartAction(target, c.Bool("force"), timeout)
			},
		},
		{
			Name:        "status",
			Usage:       "Show status of server instance(s)",
			Description: "",
			ArgsUsage:   ArgInstanceAll,
			Action: func(c *cli.Context) error {
				arg := c.Args().Get(0)
				return commands.StatusAction(arg)
			},
		},
		{
			Name:        "apply",
			Usage:       "Apply configuration and generate systemd units",
			Description: "",
			ArgsUsage:   ArgInstanceAll,
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
			ArgsUsage:   ArgInstance,
			Action: func(c *cli.Context) error {
				inst := c.Args().Get(0)
				if inst == "" {
					return showHelpSafe(c, "render-config")
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
			Action:      modsCommandHandler,
		},
	}
}

func RunApp() {
	app := NewApp()
	if err := app.Run(os.Args); err != nil {
		log.Fatalf("app error: %v", err)
	}
}
