#!/usr/bin/env python3
"""End to end for the MCP server that needs no window, `motty mcp-serve`, run as
a real child process over stdio against the loopback SSH fixture. Normally started
by tools/e2e/run_agent_mcp_e2e.sh, which first runs the window side
(agent_mcp_setup_e2e.txt) to save the hosts and trust the fixture's key:

    python3 tools/e2e/agent_mcp_e2e.py /tmp/motty-bin HOME_DIR DATA_DIR

It checks what an agent sees and can do: the tools, only the host that was opened,
commands with both output streams and their exit status, a refusal where approval
would be needed, files over SFTP, a refused unknown host key that is never written
to the trusted keys, and that the process ends when its client goes away. Every
line it writes to stdout must be protocol.
"""
import json, os, shutil, subprocess, sys, tempfile

binary, home, data = sys.argv[1:4]
failures = []


def check(name, ok, detail=""):
    print(("PASS " if ok else "FAIL ") + name + ("" if ok else f"  {detail}"))
    if not ok:
        failures.append(name)


class Server:
    def __init__(self, data_dir):
        env = dict(os.environ, HOME=home, MOTTY_DATA_DIR=data_dir, MOTTY_LANG="en")
        self.process = subprocess.Popen([binary, "mcp-serve"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, env=env, text=True, bufsize=1)
        self.next_id = 0
        self.lines = []

    def rpc(self, method, params=None):
        self.next_id += 1
        self.process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params or {}}) + "\n")
        self.process.stdin.flush()
        line = self.process.stdout.readline()
        self.lines.append(line)
        return json.loads(line)

    def call(self, name, arguments):
        reply = self.rpc("tools/call", {"name": name, "arguments": arguments})
        if "error" in reply:
            return True, reply["error"]["message"]
        result = reply["result"]
        return result.get("isError", False), "".join(part["text"] for part in result["content"])

    def close(self):
        self.process.stdin.close()
        try:
            code = self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.process.kill()
            code = None
        return code, self.process.stderr.read()


server = Server(data)
reply = server.rpc("initialize", {"protocolVersion": "2025-06-18"})
check("initialize", reply["result"]["protocolVersion"] == "2025-06-18", reply)
server.process.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")

names = {tool["name"] for tool in server.rpc("tools/list")["result"]["tools"]}
check("tools: commands and reading, no screen, no writing",
      names == {"list_sessions", "list_hosts", "terminal_exec", "terminal_exec_many", "sftp_list", "sftp_read",
                "sftp_stat", "sftp_checksum", "transfer_status"}, names)

error, text = server.call("list_sessions", {})
check("only the host opened to agents is listed", not error and "agent-host" in text and "closed-host" not in text, text)
host_id = text.split(":")[0].strip()
error, text = server.call("list_hosts", {})
check("saved hosts expose only authorized ids without connecting", not error and text.startswith(host_id + ":") and "agent-host" in text and "closed-host" not in text, text)

error, text = server.call("terminal_exec", {"command": "echo hello-from-agent"})
check("a command runs on the only host", not error and "hello-from-agent" in text and "[exit status 0]" in text, text)

error, text = server.call("terminal_exec", {"session": host_id, "command": "echo to-stdout; echo to-stderr >&2; exit 4"})
check("both streams and the exit status come back", "to-stdout" in text and "to-stderr" in text and "[exit status 4]" in text, text)

error, text = server.call("terminal_exec", {"command": "rm -rf /tmp/motty-agent-e2e"})
check("a command that needs approval is refused", error and "approval" in text, text)

error, text = server.call("terminal_exec", {"session": "closed-host", "command": "echo nope"})
check("a host that was not opened cannot be used", error and "no such host" in text and "nope" not in text, text)

error, text = server.call("sftp_list", {"path": "."})
check("files are listed over SFTP", not error and "alpha.txt" in text, text)
error, text = server.call("sftp_read", {"path": "alpha.txt"})
check("a file is read over SFTP", not error and len(text) > 0, text)

# The log keeps what was asked, never what came back: the command below prints
# 42marker, which its own text does not contain.
error, text = server.call("terminal_exec", {"command": "echo $((6*7))marker"})
check("a command's output comes back to the agent", not error and "42marker" in text, text)

code, stderr = server.close()
check("it ends when its client goes away", code == 0, (code, stderr))
check("stdout holds only protocol", all(line.strip() and json.loads(line) for line in server.lines), server.lines)

# A key that is not trusted yet is refused, and never written to the trusted keys.
fresh = tempfile.mkdtemp()
shutil.copytree(data, fresh, dirs_exist_ok=True)
open(os.path.join(fresh, "known_hosts"), "w").close()
server = Server(fresh)
server.rpc("initialize", {"protocolVersion": "2025-06-18"})
error, text = server.call("terminal_exec", {"command": "echo should-not-run"})
check("an untrusted host key is refused with what to do", error and "connect to it once from the Motty window" in text and "should-not-run" not in text, text)
check("it did not trust the key by itself", open(os.path.join(fresh, "known_hosts")).read().strip() == "")
server.close()

print("FAILED: " + ", ".join(failures) if failures else "all agent MCP checks passed")
sys.exit(1 if failures else 0)
