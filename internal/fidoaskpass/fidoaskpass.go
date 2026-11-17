// Package fidoaskpass carries the request an OpenSSH askpass helper makes to
// the running application. OpenSSH invokes SSH_ASKPASS as a child process with
// the prompt in argv, so the helper cannot call into the application directly;
// this small line protocol over a private local socket carries the prompt and
// the answer back without putting either on a command line.
package fidoaskpass

// Request is one askpass prompt. Token authenticates the helper to the
// application; it is random per agent.
type Request struct {
	Token  string `json:"token"`
	Prompt string `json:"prompt"`
}

// Response is the answer, or failure when the user cancelled.
type Response struct {
	OK       bool   `json:"ok"`
	Response string `json:"response"`
}

// Listen opens the private local channel in dir and returns its address for
// Dial. The address is a Unix socket or a Windows named pipe.
func Listen(dir string) (ListenCloser, string, error) {
	return listen(dir)
}

// Dial connects an askpass helper to the address Listen returned.
func Dial(address string) (Conn, error) {
	return dial(address)
}
