package sshconn

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh/agent"
)

// agentLink returns a client of the local ssh-agent, dialing it once and
// reusing the link for authentication and for forwarding. The link belongs to
// the owner of the connection, so it lives as long as any tab on the client.
// A missing or dead agent is an error the callers report without stopping
// anything.
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
func (conn *Conn) forwardAgent() error {
	owner := conn
	if conn.owner != nil {
		owner = conn.owner
	}
	client := conn.Client()
	if client == nil {
		return errors.New("not connected")
	}
	local, err := conn.agentLink()
	if err != nil {
		return err
	}
	owner.agentmu.Lock()
	defer owner.agentmu.Unlock()
	if owner.agentForwardClient == client {
		return nil
	}
	if err := agent.ForwardToAgent(client, local); err != nil {
		return fmt.Errorf("forward the ssh-agent: %w", err)
	}
	owner.agentForwardClient = client
	return nil
}

// AgentForwardNote is why agent forwarding is not active although it was asked
// for, or empty when it is active or was not asked for.
func (shell *Shell) AgentForwardNote() string {
	if shell.agentForwardErr == nil {
		return ""
	}
	return shell.agentForwardErr.Error()
}
