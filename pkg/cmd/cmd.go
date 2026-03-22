package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// App is the top-level command-line application. It holds registered commands
// and handles argument parsing, dispatch, and help generation.
type App struct {
	name     string
	version  string
	desc     string
	commands []*Command
	out      io.Writer
}

type config struct {
	version string
	desc    string
	out     io.Writer
}

// Option configures an App.
type Option func(*config)

// WithVersion sets the application version string shown by --version.
func WithVersion(v string) Option {
	return func(c *config) {
		c.version = v
	}
}

// WithDescription sets the application description shown in help output.
func WithDescription(d string) Option {
	return func(c *config) {
		c.desc = d
	}
}

// WithOutput sets the writer for help and version output. Defaults to
// os.Stderr.
func WithOutput(w io.Writer) Option {
	return func(c *config) {
		c.out = w
	}
}

// New creates a new App with the given name and options.
func New(name string, opts ...Option) *App {
	cfg := config{
		out: os.Stderr,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &App{
		name:    name,
		version: cfg.version,
		desc:    cfg.desc,
		out:     cfg.out,
	}
}

// Command registers a top-level command and returns it. If run is nil, the
// command is expected to have subcommands registered via Command on the
// returned Command. CommandOption values configure the command's flags.
func (a *App) Command(name, desc string, run func(*Context) error, opts ...CommandOption) *Command {
	c := newCommand(name, desc, run, opts)
	a.commands = append(a.commands, c)
	return c
}

// Run parses the given arguments and dispatches to the matching command. The
// args parameter should be os.Args (program name followed by arguments). If
// no command matches, help is printed. Commands return errors which the caller
// should handle.
func (a *App) Run(args []string) error {
	if len(args) > 0 {
		args = args[1:]
	}

	if len(args) == 0 {
		a.printHelp()
		return nil
	}

	switch args[0] {
	case "--version", "-v":
		a.printVersion()
		return nil
	case "--help", "-h":
		a.printHelp()
		return nil
	}

	name := args[0]
	for _, c := range a.commands {
		if c.name == name {
			return a.dispatch(c, args[1:], []string{a.name})
		}
	}

	return fmt.Errorf("unknown command: %s", name)
}

func (a *App) dispatch(c *Command, args []string, path []string) error {
	path = append(path, c.name)

	// Check for subcommand match before parsing flags.
	if len(c.commands) > 0 && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		for _, sub := range c.commands {
			if sub.name == args[0] {
				return a.dispatch(sub, args[1:], path)
			}
		}
		return fmt.Errorf("unknown command: %s", strings.Join(append(path, args[0]), " "))
	}

	// Scan for --help anywhere in args.
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" || arg == "-h" {
			a.printCommandHelp(c, path)
			return nil
		}
	}

	// No handler — show help.
	if c.run == nil {
		a.printCommandHelp(c, path)
		return nil
	}

	ctx, err := parseArgs(c.flags, args)
	if err != nil {
		return err
	}

	return c.run(ctx)
}

// Command represents a CLI command or subcommand. Commands with a nil run
// function serve as grouping containers for subcommands.
type Command struct {
	name     string
	desc     string
	run      func(*Context) error
	flags    []*Flag
	commands []*Command
}

// CommandOption configures a Command's flags.
type CommandOption func(*Command)

// Command registers a subcommand and returns it for further configuration.
func (c *Command) Command(name, desc string, run func(*Context) error, opts ...CommandOption) *Command {
	sub := newCommand(name, desc, run, opts)
	c.commands = append(c.commands, sub)
	return sub
}

func newCommand(name, desc string, run func(*Context) error, opts []CommandOption) *Command {
	c := &Command{
		name: name,
		desc: desc,
		run:  run,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (a *App) printHelp() {
	w := a.out
	if a.version != "" {
		fmt.Fprintf(w, "%s v%s\n", a.name, a.version)
	} else {
		fmt.Fprintln(w, a.name)
	}
	if a.desc != "" {
		fmt.Fprintf(w, "\n%s\n", a.desc)
	}
	fmt.Fprintf(w, "\nUsage:\n  %s <command> [flags]\n", a.name)

	if len(a.commands) > 0 {
		fmt.Fprintln(w, "\nCommands:")
		maxLen := 0
		for _, c := range a.commands {
			if len(c.name) > maxLen {
				maxLen = len(c.name)
			}
		}
		for _, c := range a.commands {
			fmt.Fprintf(w, "  %-*s  %s\n", maxLen, c.name, c.desc)
		}
	}

	fmt.Fprintf(w, "\nUse \"%s <command> --help\" for more information.\n", a.name)
}

func (a *App) printVersion() {
	if a.version != "" {
		fmt.Fprintf(a.out, "%s v%s\n", a.name, a.version)
	} else {
		fmt.Fprintf(a.out, "%s (no version)\n", a.name)
	}
}

func (a *App) printCommandHelp(c *Command, path []string) {
	w := a.out
	fullName := strings.Join(path, " ")

	fmt.Fprintln(w, c.desc)

	if len(c.commands) > 0 {
		fmt.Fprintf(w, "\nUsage:\n  %s <command>", fullName)
		if len(c.flags) > 0 {
			fmt.Fprint(w, " [flags]")
		}
		fmt.Fprintln(w)

		fmt.Fprintln(w, "\nCommands:")
		maxLen := 0
		for _, sub := range c.commands {
			if len(sub.name) > maxLen {
				maxLen = len(sub.name)
			}
		}
		for _, sub := range c.commands {
			fmt.Fprintf(w, "  %-*s  %s\n", maxLen, sub.name, sub.desc)
		}
	} else {
		fmt.Fprintf(w, "\nUsage:\n  %s", fullName)
		if len(c.flags) > 0 {
			fmt.Fprint(w, " [flags]")
		}
		fmt.Fprintln(w)
	}

	if len(c.flags) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		maxLen := 0
		for _, f := range c.flags {
			label := flagLabel(f)
			if len(label) > maxLen {
				maxLen = len(label)
			}
		}
		for _, f := range c.flags {
			label := flagLabel(f)
			def := ""
			if f.kind != flagBool || f.def != "false" {
				if f.def != "" {
					def = fmt.Sprintf(" (default: %s)", f.def)
				}
			}
			fmt.Fprintf(w, "  %-*s  %s%s\n", maxLen, label, f.desc, def)
		}
	}
}

func flagLabel(f *Flag) string {
	switch f.kind {
	case flagBool:
		return "--" + f.name
	case flagInt:
		return "--" + f.name + " int"
	case flagDuration:
		return "--" + f.name + " duration"
	default:
		return "--" + f.name + " string"
	}
}
