package fidoaskpass

import "net"

// ListenCloser is the listener Listen returns. It is a net.Listener on both
// supported families; naming it keeps the platform files free to return the
// concrete type.
type ListenCloser interface {
	net.Listener
}

// Conn is a helper's connection to the application.
type Conn interface {
	net.Conn
}
