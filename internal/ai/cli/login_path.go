package cli

import "time"

// LoginPathTimeout bounds how long the first CLI process waits for the user's
// login shell to report PATH. A platform that cannot read one returns at once;
// later processes reuse the value read once.
const LoginPathTimeout = 5 * time.Second
