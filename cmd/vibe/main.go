package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/sohretaga/govibe/internal/cli"
)

const usage = `vibe is the GoVibe compiler.

Usage:

	vibe check [--json]          parse and type-check the project and report diagnostics
	vibe routes [--json]         list every route, including those crud and auth generate
	vibe fmt [--check] [--json]  rewrite files in canonical format
	vibe explain <code> [--json] explain a diagnostic code, such as V201

Run vibe from the project root.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run parses the command line and runs one command in the current folder.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return cli.ExitUsage
	}
	name, args := args[0], args[1:]
	fs := flag.NewFlagSet("vibe "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print the result as one JSON object")
	switch name {
	case "check":
		if !parse(fs, args) {
			return cli.ExitUsage
		}
		return cli.Check(ctx, ".", *jsonOut, stdout, stderr)
	case "fmt":
		check := fs.Bool("check", false, "list files that are not formatted instead of rewriting them, and fail if there are any")
		if !parse(fs, args) {
			return cli.ExitUsage
		}
		return cli.Fmt(ctx, ".", *check, *jsonOut, stdout, stderr)
	case "routes":
		if !parse(fs, args) {
			return cli.ExitUsage
		}
		return cli.Routes(ctx, ".", *jsonOut, stdout, stderr)
	case "explain":
		if err := fs.Parse(args); err != nil {
			return cli.ExitUsage
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "usage: vibe explain <code> [--json], as in vibe explain V201")
			return cli.ExitUsage
		}
		return cli.Explain(fs.Arg(0), *jsonOut, stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return cli.ExitOK
	}
	fmt.Fprintf(stderr, "vibe: unknown command %q\n\n%s", name, usage)
	return cli.ExitUsage
}

func parse(fs *flag.FlagSet, args []string) bool {
	if err := fs.Parse(args); err != nil {
		return false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(fs.Output(), "%s: unexpected argument %q; run vibe from the project root\n", fs.Name(), fs.Arg(0))
		return false
	}
	return true
}
