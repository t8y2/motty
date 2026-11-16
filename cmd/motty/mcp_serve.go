//go:build darwin || windows || linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"motty/internal/datadir"
	"motty/internal/workbench"
)

// runMCPServe is the MCP server that needs no window: an MCP client starts
// `motty mcp-serve` as a child process and talks to it over stdio. It works on
// the saved hosts the user opened to agents, over connections of its own, and
// ends with its client. Only the protocol goes to stdout; what it has to say
// about itself goes to stderr.
func runMCPServe(args []string) int {
	flags := flag.NewFlagSet("mcp-serve", flag.ContinueOnError)
	dir := flags.String("data-dir", datadir.Current().Dir, "Motty configuration directory")
	var transferRoots []string
	flags.Func("transfer-root", "Allow MCP file transfers within this local directory (repeatable; requires full access)", func(value string) error {
		transferRoots = append(transferRoots, value)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: motty mcp-serve [--data-dir directory] [--transfer-root directory ...]")
		return 2
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// On Windows the client's pipes are often inherited by other processes, so
	// stdin may never reach EOF once the client exits and the server would stay
	// alive holding motty.exe open; watchParent ends it with the client. Where
	// the pipe closes by itself, watchParent does nothing.
	defer watchParent(cancel)()
	err := workbench.ServeAgentMCP(ctx, os.Stdin, os.Stdout, workbench.AgentOptions{
		KnownHostsPath: knownHostsPath(*dir),
		AIDatabasePath: aiDatabasePath(*dir),
		Keystore:       keystoreFor(*dir),
		Log:            os.Stderr,
		TransferRoots:  transferRoots,
	})
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "motty mcp-serve:", err)
		return 1
	}
	return 0
}
