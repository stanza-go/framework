package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Flag describes a command-line flag with its name, description, default value,
// and type. Flags are created using StringFlag, IntFlag, BoolFlag, and
// DurationFlag and passed as CommandOption values when registering commands.
type Flag struct {
	name string
	desc string
	def  string
	kind flagKind
}

type flagKind int

const (
	flagString flagKind = iota
	flagInt
	flagBool
	flagDuration
)

// StringFlag returns a CommandOption that adds a string flag to the command.
func StringFlag(name, def, desc string) CommandOption {
	return func(c *Command) {
		c.flags = append(c.flags, &Flag{
			name: name,
			desc: desc,
			def:  def,
			kind: flagString,
		})
	}
}

// IntFlag returns a CommandOption that adds an integer flag to the command.
func IntFlag(name string, def int, desc string) CommandOption {
	return func(c *Command) {
		c.flags = append(c.flags, &Flag{
			name: name,
			desc: desc,
			def:  strconv.Itoa(def),
			kind: flagInt,
		})
	}
}

// BoolFlag returns a CommandOption that adds a boolean flag to the command.
// Boolean flags are set to true by presence (--flag) or explicitly
// (--flag=true, --flag=false).
func BoolFlag(name string, def bool, desc string) CommandOption {
	return func(c *Command) {
		c.flags = append(c.flags, &Flag{
			name: name,
			desc: desc,
			def:  strconv.FormatBool(def),
			kind: flagBool,
		})
	}
}

// DurationFlag returns a CommandOption that adds a duration flag to the
// command. Values use Go's time.Duration syntax (e.g., "30s", "5m", "1h30m").
func DurationFlag(name string, def time.Duration, desc string) CommandOption {
	return func(c *Command) {
		c.flags = append(c.flags, &Flag{
			name: name,
			desc: desc,
			def:  def.String(),
			kind: flagDuration,
		})
	}
}

// Context provides access to parsed flags and positional arguments within a
// command handler.
type Context struct {
	flags map[string]string
	set   map[string]bool
	args  []string
}

// String returns the string value of the named flag, or an empty string if
// not found.
func (c *Context) String(name string) string {
	return c.flags[name]
}

// Int returns the integer value of the named flag, or 0 if not found or
// invalid.
func (c *Context) Int(name string) int {
	v, _ := strconv.Atoi(c.flags[name])
	return v
}

// Bool returns the boolean value of the named flag.
func (c *Context) Bool(name string) bool {
	return c.flags[name] == "true"
}

// Duration returns the duration value of the named flag, or 0 if not found
// or invalid.
func (c *Context) Duration(name string) time.Duration {
	d, _ := time.ParseDuration(c.flags[name])
	return d
}

// Has returns true if the named flag was explicitly set on the command line.
func (c *Context) Has(name string) bool {
	return c.set[name]
}

// Args returns the positional arguments remaining after flag parsing.
func (c *Context) Args() []string {
	return c.args
}

// Arg returns the positional argument at index i, or an empty string if out
// of range.
func (c *Context) Arg(i int) string {
	if i < 0 || i >= len(c.args) {
		return ""
	}
	return c.args[i]
}

// parseArgs parses command-line arguments against the defined flags and
// returns a Context with resolved values. Unknown flags produce an error.
// The "--" argument terminates flag parsing; all subsequent arguments are
// treated as positional.
func parseArgs(flags []*Flag, args []string) (*Context, error) {
	lookup := make(map[string]*Flag, len(flags))
	values := make(map[string]string, len(flags))
	for _, f := range flags {
		lookup[f.name] = f
		values[f.name] = f.def
	}

	setFlags := make(map[string]bool)
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]

		// "--" stops flag parsing.
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		// Not a flag — positional argument.
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			i++
			continue
		}

		// Strip "--" prefix.
		name := arg[2:]

		// Handle --flag=value syntax.
		var value string
		hasValue := false
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			value = name[idx+1:]
			name = name[:idx]
			hasValue = true
		}

		f, ok := lookup[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag: --%s", name)
		}

		setFlags[name] = true

		// Boolean flags: presence means true, --flag=value for explicit.
		if f.kind == flagBool {
			if hasValue {
				values[name] = value
			} else {
				values[name] = "true"
			}
			i++
			continue
		}

		// Non-bool flags require a value.
		if hasValue {
			values[name] = value
			i++
			continue
		}

		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag --%s requires a value", name)
		}
		i++
		values[name] = args[i]
		i++
	}

	return &Context{flags: values, set: setFlags, args: positional}, nil
}
