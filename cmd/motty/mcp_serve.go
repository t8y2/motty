//go:build darwin || windows || linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"motty/internal/workbench"
)

// runMCPServe is the MCP server that needs no window: an MCP client starts
// `motty mcp-serve` as a child process and talks to it over stdio. It works on
// the saved hosts the user opened to agents, over connections of its own, and
// ends with its client. Only the protocol goes to stdout; what it has to say
// about itself goes to stderr.
func runMCPServe(args []string) int {
	flags := flag.NewFlagSet("mcp-serve", flag.ContinueOnError)
	dir := flags.String("data-dir", os.Getenv(dataDirVariable), "Motty configuration directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: motty mcp-serve [--data-dir directory]")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := workbench.ServeAgentMCP(ctx, os.Stdin, os.Stdout, workbench.AgentOptions{
		KnownHostsPath: knownHostsPath(*dir),
		AIDatabasePath: aiDatabasePath(*dir),
		Log:            os.Stderr,
	})
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "motty mcp-serve:", err)
		return 1
	}
	return 0
}
