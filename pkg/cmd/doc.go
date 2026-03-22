// Package cmd provides a command-line argument parser with command registration,
// flag parsing, and automatic help generation. It is built entirely on Go's
// standard library with zero external dependencies.
//
// Basic usage:
//
//	app := cmd.New("myapp", cmd.WithVersion("1.0.0"))
//	app.Command("serve", "Start the HTTP server", handler,
//		cmd.StringFlag("host", "0.0.0.0", "Bind address"),
//		cmd.IntFlag("port", 8080, "Port to listen on"),
//	)
//	if err := app.Run(os.Args); err != nil {
//		fmt.Fprintf(os.Stderr, "error: %v\n", err)
//		os.Exit(1)
//	}
//
// Subcommands:
//
//	migrate := app.Command("migrate", "Database migrations", nil)
//	migrate.Command("up", "Run pending migrations", upHandler)
//	migrate.Command("status", "Show migration status", statusHandler)
//
// Flag syntax supports --flag value and --flag=value. Boolean flags are set
// to true by presence alone (--verbose) or explicitly (--verbose=false).
package cmd
