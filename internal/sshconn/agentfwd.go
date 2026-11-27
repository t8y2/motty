package sshconn

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentChannelType is the channel a server opens to reach the client's
// ssh-agent when one of its sessions asked for forwarding.
const agentChannelType = "auth-agent@openssh.com"

// agentLink returns a client of the local ssh-agent, dialing it once and
// reusing the link for authentication. The link belongs to the owner of the
// connection, so it lives as long as any tab on the client. A missing or dead
// agent is an error the callers report without stopping anything.
func (conn *Conn) agentLink() (agent.ExtendedAgent, error) {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	owner.agentmu.Lock()
	defer owner.agentmu.Unlock()
	if owner.agentClient != nil {
		return owner.agentClient, nil
	}
	address := agentAddress(owner.config.AgentAddress)
	if address == "" {
		return nil, errNoAgent
	}
	link, err := dialAgent(address)
	if err != nil {
		return nil, agentUnreachable(address, err)
	}
	client := agent.NewClient(link)
	owner.agentClient = client
	owner.addCloser(link)
	return client, nil
}

// forwardAgent serves the "auth-agent@openssh.com" channels of the
// connection's client from the local ssh-agent, registering the channel
// handler once per client. All tabs sharing the client share the handler.
//
// Every channel gets its own link to the agent. Password managers such as
// 1Password close a link that has been idle, so a request that arrives after
// that must dial the agent again instead of writing to the link an earlier
// shell opened; in the same way, OpenSSH forwards the agent over one
// connection per channel.
func (conn *Conn) forwardAgent() error {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	client := conn.Client()
	if client == nil {
		return errors.New("not connected")
	}
	address := agentAddress(owner.config.AgentAddress)
	owner.agentmu.Lock()
	defer owner.agentmu.Unlock()
	if owner.agentForwardClient == client {
		return nil
	}
	if address == "" {
		return errNoAgent
	}
	// Probe now so that a shell which asked for forwarding without a
	// reachable agent carries the reason; the probe itself is not kept.
	probe, err := dialAgent(address)
	if err != nil {
		return agentUnreachable(address, err)
	}
	probe.Close()
	channels := client.HandleChannelOpen(agentChannelType)
	if channels == nil {
		return fmt.Errorf("forward the ssh-agent: a handler for %s is already registered", agentChannelType)
	}
	owner.agentForwardClient = client
	go serveAgentChannels(channels, address)
	return nil
}

// serveAgentChannels accepts the forwarded agent channels of one client until
// the client closes.
func serveAgentChannels(channels <-chan ssh.NewChannel, address string) {
	for request := range channels {
		channel, requests, err := request.Accept()
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(requests)
		go serveAgentChannel(channel, address)
	}
}

// serveAgentChannel serves one forwarded channel over a link of its own, so an
// agent that drops an idle link costs only the channel that was idle, never
// the forwarding for the whole connection.
func serveAgentChannel(channel ssh.Channel, address string) {
	defer channel.Close()
	go io.Copy(io.Discard, channel.Stderr())
	link, err := dialAgent(address)
	if err != nil {
		return
	}
	defer link.Close()
	agent.ServeAgent(agent.NewClient(link), channel)
}

// AgentForwardNote is why agent forwarding is not active although it was asked
// for, or empty when it is active or was not asked for.
func (shell *Shell) AgentForwardNote() string {
	if shell.agentForwardErr == nil {
		return ""
	}
	return shell.agentForwardErr.Error()
}
