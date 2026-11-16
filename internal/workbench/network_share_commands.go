package workbench

import (
	"fmt"
	"strings"
)

var localProxyVariables = [...]string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"}

// The backup is kept in the user's shell, so stopping the tunnel never has to
// guess or overwrite proxy settings that existed before it was enabled.
func localProxyEnableCommand(port uint16) string {
	if port == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("if [ \"${MOTTY_PROXY_ACTIVE-}\" != 1 ] && [ \"${MOTTY_PROXY_ACTIVE+x}")
	for _, name := range localProxyVariables {
		fmt.Fprintf(&b, "${MOTTY_PROXY_%s_SET+x}${MOTTY_PROXY_%s_VALUE+x}", name, name)
	}
	b.WriteString("\" = '' ]; then\n")
	for _, name := range localProxyVariables {
		fmt.Fprintf(&b, "MOTTY_PROXY_%s_SET=${%s+x}; MOTTY_PROXY_%s_VALUE=${%s-}\n", name, name, name, name)
	}
	b.WriteString("MOTTY_PROXY_ACTIVE=1\nelse\n  printf '%s\\n' 'Motty proxy backup already exists; settings unchanged.' >&2\nfi\n")
	b.WriteString("if [ \"${MOTTY_PROXY_ACTIVE-}\" = 1 ]; then\n")
	for _, name := range localProxyVariables {
		fmt.Fprintf(&b, "export %s='http://127.0.0.1:%d'\n", name, port)
	}
	b.WriteString("fi")
	return b.String()
}

func localProxyDisableCommand() string {
	var b strings.Builder
	b.WriteString("if [ \"${MOTTY_PROXY_ACTIVE-}\" = 1 ]; then\n")
	for _, name := range localProxyVariables {
		fmt.Fprintf(&b, "if [ \"${MOTTY_PROXY_%s_SET-}\" = x ]; then export %s=\"${MOTTY_PROXY_%s_VALUE-}\"; else unset %s; fi\n", name, name, name, name)
		fmt.Fprintf(&b, "unset MOTTY_PROXY_%s_SET MOTTY_PROXY_%s_VALUE\n", name, name)
	}
	b.WriteString("unset MOTTY_PROXY_ACTIVE\nfi")
	return b.String()
}
