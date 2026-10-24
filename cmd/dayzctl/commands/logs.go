package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/logger"
)

// DefaultLogLines is how many recent journal entries are shown when the user
// does not specify a line count. Use `-n all` to show the whole journal for
// the unit(s).
const DefaultLogLines = "100"

// journalctlGracePeriod is how long we wait for journalctl to exit on its own
// after an interrupt before killing it.
const journalctlGracePeriod = 3 * time.Second

// ErrHelpRequested is returned by ParseLogArgs when the arguments asked for
// help instead of describing a log query.
var ErrHelpRequested = errors.New("help requested")

// LogOptions describes a journalctl query built from tail-style arguments.
type LogOptions struct {
	// Follow keeps streaming new entries (tail -f / journalctl --follow).
	Follow bool
	// Lines is the number of recent entries to show, or "all" for everything.
	Lines string
	// Since and Until optionally restrict the time range passed to journalctl.
	Since string
	Until string
	// Instances are the instance names to show logs for. Empty means "all".
	Instances []string
}

// LogsAction shows (and optionally follows) the systemd journal for one or
// more DayZ instances. It mirrors `journalctl --follow -u dayz@<name>.service`
// but accepts tail-style options such as `-f`, `-n 200` and `-200f`.
func LogsAction(opts LogOptions) error {
	units, err := resolveLogUnits(opts.Instances)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("journalctl"); err != nil {
		return fmt.Errorf("journalctl not found in PATH; logs require a systemd-based host")
	}

	args := buildJournalctlArgs(opts, units)
	logger.Debug("Querying journal", "units", strings.Join(units, ","), "args", strings.Join(args, " "))
	return runJournalctl(args)
}

// resolveLogUnits maps instance names (or "all") to systemd unit names.
func resolveLogUnits(targets []string) ([]string, error) {
	if len(targets) == 0 {
		targets = []string{"all"}
	}

	seen := make(map[string]struct{}, len(targets))
	units := make([]string, 0, len(targets))
	for _, target := range targets {
		instances, err := shared.GetInstances(target)
		if err != nil {
			return nil, err
		}
		for _, inst := range instances {
			unit := "dayz@" + inst.Name + ".service"
			if _, ok := seen[unit]; ok {
				continue
			}
			seen[unit] = struct{}{}
			units = append(units, unit)
		}
	}

	if len(units) == 0 {
		return nil, fmt.Errorf("no instances selected")
	}
	return units, nil
}

// buildJournalctlArgs translates LogOptions into journalctl arguments.
func buildJournalctlArgs(opts LogOptions, units []string) []string {
	args := make([]string, 0, 2*len(units)+6)
	for _, unit := range units {
		args = append(args, "--unit", unit)
	}
	if opts.Lines != "" && !strings.EqualFold(opts.Lines, "all") {
		args = append(args, "--lines", opts.Lines)
	}
	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}
	if opts.Until != "" {
		args = append(args, "--until", opts.Until)
	}
	if opts.Follow {
		args = append(args, "--follow")
	}
	return args
}

// journalctlCommand builds the exec.Cmd used to read the journal. It is a
// package variable so tests can substitute a fake command.
var journalctlCommand = func(args ...string) *exec.Cmd {
	return exec.Command("journalctl", args...)
}

// runJournalctl runs journalctl attached to the current terminal, so log
// output streams straight to the user. Interrupts (Ctrl+C) end the stream
// quietly rather than reporting an error.
func runJournalctl(args []string) error {
	cmd := journalctlCommand(args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Catch interrupts so a follow session exits cleanly. journalctl runs in
	// the same process group and receives the terminal's SIGINT as well.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start journalctl: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case err := <-waitCh:
		return journalctlResult(err)
	case <-sigCh:
		select {
		case <-waitCh:
		case <-time.After(journalctlGracePeriod):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitCh
		}
		return nil
	}
}

// journalctlResult converts a journalctl exit into a user-facing error. A
// signal-terminated run (e.g. the pager being quit with Ctrl+C) is clean.
func journalctlResult(err error) error {
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ProcessState != nil && exitErr.ProcessState.ExitCode() == -1 {
			return nil
		}
		if strings.Contains(err.Error(), "signal:") {
			return nil
		}
	}
	return fmt.Errorf("journalctl failed: %w", err)
}

// ParseLogArgs parses tail-style arguments for the logs command:
//
//	-n 200, -n200, -200      last 200 entries (-n all for everything)
//	-f, --follow             stream new entries
//	-200f, -fn 50            tail-style combinations
//	--since/--until <when>   journalctl time filters
//	[instance...]            instances to show (use "all" for every instance)
//
// It returns ErrHelpRequested when no arguments are given (bare `dayzctl
// logs` prints help, like the other top-level commands) or when
// -h/--help/help is present.
func ParseLogArgs(args []string) (LogOptions, error) {
	opts := LogOptions{Lines: DefaultLogLines}

	if len(args) == 0 {
		return opts, ErrHelpRequested
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			opts.Instances = append(opts.Instances, args[i+1:]...)
			i = len(args)

		case arg == "-h" || arg == "--help" || arg == "help":
			return opts, ErrHelpRequested

		case strings.HasPrefix(arg, "--"):
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--follow":
				if hasValue {
					return opts, fmt.Errorf("option %s does not take a value", name)
				}
				opts.Follow = true
			case "--lines", "--since", "--until":
				if !hasValue {
					if i+1 >= len(args) {
						return opts, fmt.Errorf("option %s requires a value", name)
					}
					i++
					value = args[i]
				}
				if err := applyLogValue(&opts, name, value); err != nil {
					return opts, err
				}
			default:
				return opts, fmt.Errorf("unknown option: %s", name)
			}

		case len(arg) > 1 && arg[0] == '-':
			if err := parseShortLogFlags(&opts, arg[1:], args, &i); err != nil {
				return opts, err
			}

		default:
			opts.Instances = append(opts.Instances, arg)
		}
	}

	return opts, nil
}

// applyLogValue stores a long-option value on opts.
func applyLogValue(opts *LogOptions, name, value string) error {
	switch name {
	case "--lines":
		return applyLogLines(opts, value)
	case "--since":
		opts.Since = value
	case "--until":
		opts.Until = value
	}
	return nil
}

// applyLogLines validates and stores a line count (or "all").
func applyLogLines(opts *LogOptions, value string) error {
	if strings.EqualFold(value, "all") {
		opts.Lines = "all"
		return nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid line count %q: use a non-negative number or 'all'", value)
	}
	opts.Lines = strconv.Itoa(n)
	return nil
}

// parseShortLogFlags parses one tail-style short-option cluster such as "f",
// "n50", "200f" or "fn 50". i points at the argument being parsed and is
// advanced when a value is taken from the next argument.
func parseShortLogFlags(opts *LogOptions, body string, args []string, i *int) error {
	nextValue := func() (string, bool) {
		if *i+1 >= len(args) {
			return "", false
		}
		*i++
		return args[*i], true
	}

	for pos := 0; pos < len(body); {
		c := body[pos]
		switch {
		case c >= '0' && c <= '9':
			end := pos
			for end < len(body) && body[end] >= '0' && body[end] <= '9' {
				end++
			}
			if err := applyLogLines(opts, body[pos:end]); err != nil {
				return err
			}
			pos = end

		case c == 'f':
			opts.Follow = true
			pos++

		case c == 'n':
			if rest := body[pos+1:]; rest != "" {
				if err := applyLogLines(opts, rest); err != nil {
					return err
				}
				return nil
			}
			value, ok := nextValue()
			if !ok {
				return fmt.Errorf("option -n requires a value")
			}
			if err := applyLogLines(opts, value); err != nil {
				return err
			}
			return nil

		default:
			return fmt.Errorf("unknown option: -%c", c)
		}
	}

	return nil
}
