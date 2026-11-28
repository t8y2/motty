//go:build darwin || windows || linux

package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/vivaui/viva/diagnostics"
)

// logFrameworkEvents routes Viva's diagnostic stream into the program's log: a
// graphics device that was lost or could not be made again, the path the
// renderer took, a web view that failed to start. motty.log is what a bug
// report comes with, so what the framework worked around belongs there too.
func logFrameworkEvents() {
	diagnostics.SetHandler(func(event diagnostics.Event) {
		var line strings.Builder
		fmt.Fprintf(&line, "viva %s %s: %s", event.Subsystem, event.Code, event.Message)
		for _, attr := range event.Attrs {
			fmt.Fprintf(&line, " %s=%s", attr.Key, attr.Value.String())
		}
		log.Print(line.String())
	})
}
