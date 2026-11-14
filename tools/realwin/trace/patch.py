"""Insert timing probes into scratch copies of viva and motty. Fails loudly when an anchor
has moved, instead of silently producing an uninstrumented build."""
import sys
from pathlib import Path

viva, motty = Path(sys.argv[1]), Path(sys.argv[2])

CLOCK = 'float64(time.Now().UnixMicro()%1000000000)/1000'


def tracer(package: str, func: str, env: str) -> str:
    return f'''package {package}

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	{func}Mu   sync.Mutex
	{func}File *os.File
)

func {func}(format string, args ...any) {{
	path := os.Getenv("{env}")
	if path == "" {{
		return
	}}
	{func}Mu.Lock()
	defer {func}Mu.Unlock()
	if {func}File == nil {{
		{func}File, _ = os.Create(path)
	}}
	fmt.Fprintf({func}File, "%9.3f ", {CLOCK})
	fmt.Fprintf({func}File, format+"\\n", args...)
}}
'''


def edit(path: Path, old: str, new: str, count: int = 1) -> None:
    text = path.read_text()
    if old not in text:
        sys.exit(f"patch anchor not found in {path}:\n{old}")
    path.write_text(text.replace(old, new, count))


win = viva / "internal/platform/windows"
(win / "trace_windows.go").write_text(tracer("windows", "trace", "VIVA_TRACE"))
edit(win / "window_windows.go",
     "				posted, _, _ := postMessage.Call(state.window, wmRedraw, 0, 0)",
     '				trace("post")\n				posted, _, _ := postMessage.Call(state.window, wmRedraw, 0, 0)')
edit(win / "window_windows.go",
     "			frame, redrawErr := state.config.OnRedraw()\n			state.frame(frame, redrawErr)",
     '			trace("redraw-begin")\n			frame, redrawErr := state.config.OnRedraw()\n			trace("redraw-onredraw-done")\n			state.frame(frame, redrawErr)\n			trace("redraw-end")')
edit(win / "window_windows.go",
     "		err := state.present(int(bounds.Right), int(bounds.Bottom))\n",
     '		trace("paint-begin")\n		err := state.present(int(bounds.Right), int(bounds.Bottom))\n		trace("paint-presented")\n')
edit(win / "window_windows.go", "	case wmChar:\n", '	case wmChar:\n		trace("char %d", first)\n')
edit(win / "window_windows.go", "	case wmKeyDown, wmSysKeyDown:\n", '	case wmKeyDown, wmSysKeyDown:\n		trace("keydown %d", first)\n')
edit(win / "renderer_windows.go",
     "	return hresult(comCall(renderer.swap, 8, sync, 0))\n}",
     '	trace("draw-done")\n	status := comCall(renderer.swap, 8, sync, 0)\n	trace("Present-returned")\n	return hresult(status)\n}')

(motty / "internal/workbench/trace_tmp.go").write_text(tracer("workbench", "traceTmp", "MOTTY_PUMP_TRACE"))
edit(motty / "internal/workbench/terminal.go",
     "	data := t.rx\n	wasExec := t.exec != nil\n",
     '	data := t.rx\n	if len(data) > 0 {\n		traceTmp("pump drained=%d", len(data))\n	}\n	wasExec := t.exec != nil\n')

(motty / "internal/localshell/trace_tmp.go").write_text(tracer("localshell", "trace", "MOTTY_SHELL_TRACE"))
shell = motty / "internal/localshell/shell.go"
edit(shell, "		if n > 0 {\n			shell.mu.Lock()", '		if n > 0 {\n			trace("read n=%d", n)\n			shell.mu.Lock()')
edit(shell, "		if _, err := shell.b.io.Write(data); err != nil {", '		trace("write-begin n=%d", len(data))\n		if _, err := shell.b.io.Write(data); err != nil {')
edit(shell, "	select {\n	case shell.input <- append", '	trace("Write called n=%d", len(p))\n	select {\n	case shell.input <- append')
print("patched")
