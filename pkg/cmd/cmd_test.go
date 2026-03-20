package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	app := New("myapp")
	if app.name != "myapp" {
		t.Errorf("name = %q, want %q", app.name, "myapp")
	}
	if app.out == nil {
		t.Error("out is nil, want default writer")
	}
}

func TestNewWithOptions(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp",
		WithVersion("1.2.3"),
		WithDescription("My app"),
		WithOutput(&buf),
	)
	if app.version != "1.2.3" {
		t.Errorf("version = %q, want %q", app.version, "1.2.3")
	}
	if app.desc != "My app" {
		t.Errorf("desc = %q, want %q", app.desc, "My app")
	}
}

func TestVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		args    []string
		want    string
	}{
		{"long flag", "1.0.0", []string{"app", "--version"}, "app v1.0.0\n"},
		{"short flag", "2.5.1", []string{"app", "-v"}, "app v2.5.1\n"},
		{"no version", "", []string{"app", "--version"}, "app (no version)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			app := New("app", WithVersion(tt.version), WithOutput(&buf))
			if err := app.Run(tt.args); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHelpNoArgs(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithVersion("1.0.0"), WithDescription("A test app"), WithOutput(&buf))
	app.Command("serve", "Start the server", noop)
	app.Command("migrate", "Run migrations", noop)

	if err := app.Run([]string{"myapp"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	assertContains(t, out, "myapp v1.0.0")
	assertContains(t, out, "A test app")
	assertContains(t, out, "serve")
	assertContains(t, out, "migrate")
	assertContains(t, out, "Start the server")
	assertContains(t, out, "Run migrations")
}

func TestHelpFlag(t *testing.T) {
	tests := []string{"--help", "-h"}
	for _, flag := range tests {
		t.Run(flag, func(t *testing.T) {
			var buf bytes.Buffer
			app := New("myapp", WithVersion("1.0.0"), WithOutput(&buf))
			app.Command("serve", "Start the server", noop)

			if err := app.Run([]string{"myapp", flag}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertContains(t, buf.String(), "serve")
		})
	}
}

func TestCommandHelp(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	app.Command("serve", "Start the HTTP server", noop,
		StringFlag("host", "0.0.0.0", "Bind address"),
		IntFlag("port", 8080, "Port to listen on"),
		BoolFlag("debug", false, "Enable debug mode"),
		DurationFlag("timeout", 30*time.Second, "Request timeout"),
	)

	if err := app.Run([]string{"myapp", "serve", "--help"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	assertContains(t, out, "Start the HTTP server")
	assertContains(t, out, "--host string")
	assertContains(t, out, "(default: 0.0.0.0)")
	assertContains(t, out, "--port int")
	assertContains(t, out, "(default: 8080)")
	assertContains(t, out, "--debug")
	assertContains(t, out, "--timeout duration")
	assertContains(t, out, "(default: 30s)")

	// Bool with default false should not show default.
	if strings.Contains(out, "(default: false)") {
		t.Error("bool flag with default false should not show (default: false)")
	}
}

func TestCommandHelpInMiddleOfArgs(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	app.Command("serve", "Start the server", noop,
		StringFlag("host", "0.0.0.0", "Bind address"),
	)

	if err := app.Run([]string{"myapp", "serve", "--host", "localhost", "--help"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertContains(t, buf.String(), "Start the server")
}

func TestSubcommandHelp(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	migrate := app.Command("migrate", "Database migrations", nil)
	migrate.Command("up", "Run pending migrations", noop)
	migrate.Command("status", "Show migration status", noop)

	if err := app.Run([]string{"myapp", "migrate"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	assertContains(t, out, "Database migrations")
	assertContains(t, out, "up")
	assertContains(t, out, "status")
}

func TestSubcommandDispatch(t *testing.T) {
	var ran string
	app := New("myapp", WithOutput(discard))
	migrate := app.Command("migrate", "Migrations", nil)
	migrate.Command("up", "Up", func(c *Context) error {
		ran = "up"
		return nil
	})
	migrate.Command("status", "Status", func(c *Context) error {
		ran = "status"
		return nil
	})

	if err := app.Run([]string{"myapp", "migrate", "up"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran != "up" {
		t.Errorf("ran = %q, want %q", ran, "up")
	}

	ran = ""
	if err := app.Run([]string{"myapp", "migrate", "status"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran != "status" {
		t.Errorf("ran = %q, want %q", ran, "status")
	}
}

func TestFlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantHost string
		wantPort int
	}{
		{
			"space separated",
			[]string{"myapp", "serve", "--host", "localhost", "--port", "3000"},
			"localhost", 3000,
		},
		{
			"equals syntax",
			[]string{"myapp", "serve", "--host=127.0.0.1", "--port=9090"},
			"127.0.0.1", 9090,
		},
		{
			"mixed syntax",
			[]string{"myapp", "serve", "--host=0.0.0.0", "--port", "4000"},
			"0.0.0.0", 4000,
		},
		{
			"defaults",
			[]string{"myapp", "serve"},
			"0.0.0.0", 8080,
		},
		{
			"partial override",
			[]string{"myapp", "serve", "--port", "5000"},
			"0.0.0.0", 5000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotHost string
			var gotPort int
			app := New("myapp", WithOutput(discard))
			app.Command("serve", "Serve", func(c *Context) error {
				gotHost = c.String("host")
				gotPort = c.Int("port")
				return nil
			},
				StringFlag("host", "0.0.0.0", "Host"),
				IntFlag("port", 8080, "Port"),
			)

			if err := app.Run(tt.args); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if gotHost != tt.wantHost {
				t.Errorf("host = %q, want %q", gotHost, tt.wantHost)
			}
			if gotPort != tt.wantPort {
				t.Errorf("port = %d, want %d", gotPort, tt.wantPort)
			}
		})
	}
}

func TestBoolFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"presence", []string{"myapp", "run", "--verbose"}, true},
		{"explicit true", []string{"myapp", "run", "--verbose=true"}, true},
		{"explicit false", []string{"myapp", "run", "--verbose=false"}, false},
		{"default false", []string{"myapp", "run"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			app := New("myapp", WithOutput(discard))
			app.Command("run", "Run", func(c *Context) error {
				got = c.Bool("verbose")
				return nil
			}, BoolFlag("verbose", false, "Verbose"))

			if err := app.Run(tt.args); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got != tt.want {
				t.Errorf("verbose = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDurationFlag(t *testing.T) {
	var got time.Duration
	app := New("myapp", WithOutput(discard))
	app.Command("run", "Run", func(c *Context) error {
		got = c.Duration("timeout")
		return nil
	}, DurationFlag("timeout", 10*time.Second, "Timeout"))

	if err := app.Run([]string{"myapp", "run", "--timeout", "5m"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 5*time.Minute {
		t.Errorf("timeout = %v, want 5m", got)
	}
}

func TestDurationFlagDefault(t *testing.T) {
	var got time.Duration
	app := New("myapp", WithOutput(discard))
	app.Command("run", "Run", func(c *Context) error {
		got = c.Duration("timeout")
		return nil
	}, DurationFlag("timeout", 30*time.Second, "Timeout"))

	if err := app.Run([]string{"myapp", "run"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", got)
	}
}

func TestPositionalArgs(t *testing.T) {
	var gotArgs []string
	app := New("myapp", WithOutput(discard))
	app.Command("import", "Import", func(c *Context) error {
		gotArgs = c.Args()
		return nil
	})

	if err := app.Run([]string{"myapp", "import", "backup.zip", "extra"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "backup.zip" || gotArgs[1] != "extra" {
		t.Errorf("args = %v, want [backup.zip extra]", gotArgs)
	}
}

func TestDoubleDash(t *testing.T) {
	var gotHost string
	var gotArgs []string
	app := New("myapp", WithOutput(discard))
	app.Command("run", "Run", func(c *Context) error {
		gotHost = c.String("host")
		gotArgs = c.Args()
		return nil
	}, StringFlag("host", "0.0.0.0", "Host"))

	if err := app.Run([]string{"myapp", "run", "--host", "localhost", "--", "--not-a-flag", "arg"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotHost != "localhost" {
		t.Errorf("host = %q, want %q", gotHost, "localhost")
	}
	if len(gotArgs) != 2 || gotArgs[0] != "--not-a-flag" || gotArgs[1] != "arg" {
		t.Errorf("args = %v, want [--not-a-flag arg]", gotArgs)
	}
}

func TestContextArg(t *testing.T) {
	var got0, got1, got2 string
	app := New("myapp", WithOutput(discard))
	app.Command("import", "Import", func(c *Context) error {
		got0 = c.Arg(0)
		got1 = c.Arg(1)
		got2 = c.Arg(2)
		return nil
	})

	if err := app.Run([]string{"myapp", "import", "a", "b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got0 != "a" {
		t.Errorf("Arg(0) = %q, want %q", got0, "a")
	}
	if got1 != "b" {
		t.Errorf("Arg(1) = %q, want %q", got1, "b")
	}
	if got2 != "" {
		t.Errorf("Arg(2) = %q, want %q", got2, "")
	}
}

func TestContextArgNegativeIndex(t *testing.T) {
	app := New("myapp", WithOutput(discard))
	app.Command("run", "Run", func(c *Context) error {
		if got := c.Arg(-1); got != "" {
			t.Errorf("Arg(-1) = %q, want %q", got, "")
		}
		return nil
	})

	if err := app.Run([]string{"myapp", "run"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestContextHas(t *testing.T) {
	var hasHost, hasPort bool
	app := New("myapp", WithOutput(discard))
	app.Command("serve", "Serve", func(c *Context) error {
		hasHost = c.Has("host")
		hasPort = c.Has("port")
		return nil
	},
		StringFlag("host", "0.0.0.0", "Host"),
		IntFlag("port", 8080, "Port"),
	)

	if err := app.Run([]string{"myapp", "serve", "--host", "localhost"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasHost {
		t.Error("Has(host) = false, want true")
	}
	if hasPort {
		t.Error("Has(port) = true, want false")
	}
}

func TestUnknownCommand(t *testing.T) {
	app := New("myapp", WithOutput(discard))
	app.Command("serve", "Serve", noop)

	err := app.Run([]string{"myapp", "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	assertContains(t, err.Error(), "unknown command")
}

func TestUnknownSubcommand(t *testing.T) {
	app := New("myapp", WithOutput(discard))
	migrate := app.Command("migrate", "Migrations", nil)
	migrate.Command("up", "Up", noop)

	err := app.Run([]string{"myapp", "migrate", "down"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	assertContains(t, err.Error(), "unknown command")
	assertContains(t, err.Error(), "down")
}

func TestUnknownFlag(t *testing.T) {
	app := New("myapp", WithOutput(discard))
	app.Command("serve", "Serve", noop,
		StringFlag("host", "0.0.0.0", "Host"),
	)

	err := app.Run([]string{"myapp", "serve", "--port", "8080"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	assertContains(t, err.Error(), "unknown flag")
	assertContains(t, err.Error(), "port")
}

func TestMissingFlagValue(t *testing.T) {
	app := New("myapp", WithOutput(discard))
	app.Command("serve", "Serve", noop,
		StringFlag("host", "0.0.0.0", "Host"),
	)

	err := app.Run([]string{"myapp", "serve", "--host"})
	if err == nil {
		t.Fatal("expected error for missing flag value")
	}
	assertContains(t, err.Error(), "requires a value")
}

func TestCommandReturnsError(t *testing.T) {
	app := New("myapp", WithOutput(discard))
	app.Command("fail", "Fail", func(c *Context) error {
		return errors.New("something broke")
	})

	err := app.Run([]string{"myapp", "fail"})
	if err == nil {
		t.Fatal("expected error from command")
	}
	if err.Error() != "something broke" {
		t.Errorf("error = %q, want %q", err.Error(), "something broke")
	}
}

func TestNoHandlerShowsHelp(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	migrate := app.Command("migrate", "Database migrations", nil)
	migrate.Command("up", "Run pending", noop)

	if err := app.Run([]string{"myapp", "migrate"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContains(t, buf.String(), "Database migrations")
	assertContains(t, buf.String(), "up")
}

func TestBoolFlagDefaultTrue(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	app.Command("run", "Run", noop, BoolFlag("color", true, "Enable color"))

	if err := app.Run([]string{"myapp", "run", "--help"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Bool with default true should show the default.
	assertContains(t, buf.String(), "(default: true)")
}

func TestFlagsWithPositionalArgs(t *testing.T) {
	var gotHost string
	var gotArgs []string
	app := New("myapp", WithOutput(discard))
	app.Command("run", "Run", func(c *Context) error {
		gotHost = c.String("host")
		gotArgs = c.Args()
		return nil
	}, StringFlag("host", "0.0.0.0", "Host"))

	if err := app.Run([]string{"myapp", "run", "file.txt", "--host", "localhost", "other.txt"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotHost != "localhost" {
		t.Errorf("host = %q, want %q", gotHost, "localhost")
	}
	if len(gotArgs) != 2 || gotArgs[0] != "file.txt" || gotArgs[1] != "other.txt" {
		t.Errorf("args = %v, want [file.txt other.txt]", gotArgs)
	}
}

func TestEmptyArgs(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	app.Command("serve", "Serve", noop)

	if err := app.Run(nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertContains(t, buf.String(), "myapp")
}

func TestSubcommandWithFlags(t *testing.T) {
	var gotN int
	app := New("myapp", WithOutput(discard))
	migrate := app.Command("migrate", "Migrations", nil)
	migrate.Command("up", "Run migrations", func(c *Context) error {
		gotN = c.Int("steps")
		return nil
	}, IntFlag("steps", 0, "Number of steps"))

	if err := app.Run([]string{"myapp", "migrate", "up", "--steps", "5"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotN != 5 {
		t.Errorf("steps = %d, want %d", gotN, 5)
	}
}

func TestSubcommandFlagHelp(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	migrate := app.Command("migrate", "Migrations", nil)
	migrate.Command("up", "Run pending", noop, IntFlag("steps", 0, "Steps"))

	if err := app.Run([]string{"myapp", "migrate", "up", "--help"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	assertContains(t, out, "Run pending")
	assertContains(t, out, "--steps int")
}

func TestHelpAlignment(t *testing.T) {
	var buf bytes.Buffer
	app := New("myapp", WithOutput(&buf))
	app.Command("serve", "Short", noop)
	app.Command("migrate", "Longer command", noop)

	if err := app.Run([]string{"myapp", "--help"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Both descriptions should be aligned at the same column.
	lines := strings.Split(buf.String(), "\n")
	var descPositions []int
	for _, line := range lines {
		if idx := strings.Index(line, "Short"); idx >= 0 {
			descPositions = append(descPositions, idx)
		}
		if idx := strings.Index(line, "Longer command"); idx >= 0 {
			descPositions = append(descPositions, idx)
		}
	}
	if len(descPositions) == 2 && descPositions[0] != descPositions[1] {
		t.Errorf("descriptions not aligned: positions %v", descPositions)
	}
}

func TestContextGettersZeroValues(t *testing.T) {
	ctx := &Context{
		flags: map[string]string{},
		set:   map[string]bool{},
	}

	if got := ctx.String("missing"); got != "" {
		t.Errorf("String(missing) = %q, want %q", got, "")
	}
	if got := ctx.Int("missing"); got != 0 {
		t.Errorf("Int(missing) = %d, want 0", got)
	}
	if got := ctx.Bool("missing"); got != false {
		t.Errorf("Bool(missing) = %v, want false", got)
	}
	if got := ctx.Duration("missing"); got != 0 {
		t.Errorf("Duration(missing) = %v, want 0", got)
	}
	if got := ctx.Has("missing"); got != false {
		t.Errorf("Has(missing) = %v, want false", got)
	}
}

// noop is a command handler that does nothing.
func noop(_ *Context) error { return nil }

// discard is an io.Writer that discards all output.
var discard = &bytes.Buffer{}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("output does not contain %q:\n%s", substr, s)
	}
}
