#!/usr/bin/env python3
"""End to end for the MCP server (Settings > MCP) and its stdio
bridge, against the loopback SSH fixture on port 2222:

    cd tools/e2esshd && go build -o /tmp/e2esshd . && /tmp/e2esshd 2222 &
    go build -tags vivahead -o /tmp/motty-head ./cmd/motty
    go build -o /tmp/motty-bin ./cmd/motty
    python3 tools/e2e/mcp_e2e.py

Chinese labels; coordinates are logical pixels of the 1400x900 window. It turns
the server on, reads the address and token from the copied client command, and
checks: authentication, tools, read-only mode refusing commands, ask mode
running a low-risk command at once, waiting for approval for a risky one and
honoring Approve and Deny, and the stdio bridge speaking to the same server.
"""
import json, os, re, subprocess, sys, tempfile, threading, time, urllib.error, urllib.request

home = tempfile.mkdtemp()
data = os.path.join(home, "motty-data")
os.makedirs(data, exist_ok=True)
env = dict(os.environ, VIVA_HEADLESS="1", HOME=home, MOTTY_DATA_DIR=data)
proc = subprocess.Popen(["/tmp/motty-head"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, env=env, text=True, bufsize=1)
assert proc.stdout.readline().startswith("ok ready")
failures = []


def check(name, ok, detail=""):
    print(("PASS " if ok else "FAIL ") + name + ("" if ok else f"  {detail}"))
    if not ok:
        failures.append(name)


def cmd(c):
    proc.stdin.write(c + "\n"); proc.stdin.flush()
    out = []
    while True:
        line = proc.stdout.readline()
        if line == "":
            sys.exit("process ended: " + proc.stderr.read())
        line = line.rstrip("\n")
        if line.startswith(("ok", "error")):
            return "\n".join(out), line
        out.append(line)


for c in ['click "设置"', 'click "MCP"', 'click 1230 224', 'settle 500', 'click "复制配置"', 'settle 300']:
    cmd(c)
_, reply = cmd("clip")
m = re.search(r"(http://127\.0\.0\.1:\d+/mcp)", reply)
t = re.search(r"Bearer ([^\"\s]+)", reply)
if not (m and t):
    sys.exit("no address/token in the copied client command")
url, token = m.group(1), t.group(1).rstrip("\\")
mcp_session = ""


def rpc(method, params=None, tok=token, timeout=30):
    global mcp_session
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params or {}}).encode()
    headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
    if tok:
        headers["Authorization"] = "Bearer " + tok
    if mcp_session and method != "initialize":
        headers["Mcp-Session-Id"] = mcp_session
    try:
        with urllib.request.urlopen(urllib.request.Request(url, body, headers), timeout=timeout) as r:
            if method == "initialize" and tok == token:
                mcp_session = r.headers.get("Mcp-Session-Id", "")
            return r.status, r.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()


def call(name, args, timeout=40):
    status, data = rpc("tools/call", {"name": name, "arguments": args}, timeout=timeout)
    if status != 200:
        return status, data
    result = json.loads(data)["result"]
    return result.get("isError", False), "".join(c["text"] for c in result["content"])


def pumped(name, args, ms=1500):
    box = {}
    th = threading.Thread(target=lambda: box.update(r=call(name, args)))
    th.start(); cmd(f"settle {ms}")
    th.join(timeout=10)
    return box.get("r")


check("no token is refused", rpc("initialize", tok="")[0] == 401)
check("a wrong token is refused", rpc("initialize", tok="x" * 30)[0] == 401)
status, data = rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "t", "version": "1"}})
check("initialize", status == 200 and "motty" in data, data[:100])
names = [x["name"] for x in json.loads(rpc("tools/list")[1])["result"]["tools"]]
check("tools are listed", "terminal_exec" in names and "sftp_list" in names and "list_hosts" in names, names)
err, text = pumped("list_hosts", {})
check("no unapproved hosts are exposed", not err and text == "no hosts are open to agents", text)

for c in ['click "主机"', 'click 462 68', 'type tester@127.0.0.1:2222', 'click "连接"', 'waitfor "信任" 5000', 'click "信任并保存"',
          'waitfor "密码" 5000', 'type secret', 'key enter', 'waitfor "tester@e2e" 8000']:
    cmd(c)
err, text = pumped("list_sessions", {})
sid = re.search(r"(w\d+:\d+)", text).group(1)
check("a session is listed", not err and "127.0.0.1:2222" in text, text)
err, text = pumped("terminal_read", {"session": sid})
check("the screen is readable", not err and "tester@e2e" in text, text[:100])
err, text = pumped("terminal_exec", {"session": sid, "command": "echo hi"})
check("read-only mode refuses commands", err is True and "read-only" in text, text)
err, text = pumped("sftp_list", {"session": sid, "path": "."})
check("sftp_list works read-only", not err and "alpha.txt" in text, text)

# ask mode: low risk runs at once, risky waits for the window
for c in ['click "设置"', 'click "MCP"', 'click 1150 318']:
    cmd(c)
check("ask mode establishes a fresh MCP session", rpc("initialize", {"protocolVersion": "2025-06-18"})[0] == 200 and bool(mcp_session))
err, text = pumped("terminal_exec", {"session": sid, "command": "echo hi-$((6*7))"})
check("a low-risk command runs without asking", not err and "hi-42" in text, text)

line = "0" * 64 + " 中文日志"
command = "seq 1500|awk '{printf \"M:%04d:%064d 中文日志\\n\",$1,0}'"
result = pumped("terminal_exec", {"session": sid, "command": command}, ms=3000)
if result:
    failed, preview = result
    match = re.search(r"output_id: ([^;]+); total_bytes: (\d+)", preview)
    check("large SSH output keeps a bounded preview and a readback handle",
          not failed and match is not None and "M:0750:" not in preview and "[exit status 0]" in preview, preview[:300])
    if match:
        output_id = match.group(1)
        collected = ""
        offset = 0
        for _ in range(5):
            page = pumped("tool_output_read", {"output_id": output_id, "offset": offset, "max_bytes": 65536}, ms=500)
            if not page or page[0]:
                break
            page = json.loads(page[1])
            collected += page["data"]
            offset = page["next_offset"]
            if page["eof"]:
                break
        expected = "\n".join(f"M:{i:04d}:{line}" for i in range(1, 1501))
        check("SSH output pages restore the middle without rerunning the command", collected == expected,
              f"recovered {len(collected)} of {len(expected)} characters; prefix {collected[:190]!r}; suffix {collected[-130:]!r}")
else:
    check("large SSH output keeps a bounded preview and a readback handle", False, "no response")


def risky(decision, command="mkdir -p /tmp/motty-mcp-t && rm -rf /tmp/motty-mcp-t"):
    box = {}
    th = threading.Thread(target=lambda: box.update(r=call("terminal_exec", {"session": sid, "command": command})))
    th.start(); time.sleep(0.5); cmd("settle 1500")
    waiting = th.is_alive()
    texts, _ = cmd("texts")
    if "需要你的批准" not in texts:  # the AI panel toggles; once open it stays open
        cmd("click 1224 18"); cmd("settle 800")
        texts, _ = cmd("texts")
    asked = "需要你的批准" in texts
    cmd(f'click "{decision}"'); cmd("settle 2000")
    th.join(timeout=10)
    return waiting, asked, box.get("r")


waiting, asked, result = risky("批准")
check("a risky command waits and asks", waiting and asked)
check("Approve runs it", result and result[0] is False and "exit status 0" in result[1], result)
waiting, asked, result = risky("拒绝")
check("Deny is reported to the agent", result and result[0] is True and "exit status" not in result[1], result)

original_command = "python3 -c 'for i in range(1,1501): print(\"MCPLOG:%04d:ERROR connection_timeout 中文日志 %s\" % (i,\"x\"*64))'"
waiting, asked, result = risky("批准", original_command)
match = re.search(r"output_id: ([^;]+); total_bytes: (\d+)", result[1]) if result else None
check("original issue command returns a readback handle after approval", waiting and asked and result[0] is False and match is not None,
      str(result)[:300])
if match:
    collected = ""
    offset = 0
    for _ in range(5):
        page = pumped("tool_output_read", {"output_id": match.group(1), "offset": offset, "max_bytes": 65536}, ms=500)
        if not page or page[0]:
            break
        page = json.loads(page[1])
        collected += page["data"]
        offset = page["next_offset"]
        if page["eof"]:
            break
    numbers = [int(n) for n in re.findall(r"^MCPLOG:(\d{4}):", collected, re.MULTILINE)]
    check("original issue command restores all 1500 numbered lines",
          numbers == list(range(1, 1501)), f"complete lines={len(numbers)}, first={numbers[:2]}, last={numbers[-2:]}")
    check("original issue command does not expose its shell wrapper",
          collected.startswith("MCPLOG:0001:") and "GOPAI_" not in collected, repr(collected[:220]))

# the stdio bridge
bridge = subprocess.Popen(["/tmp/motty-bin", "mcp-bridge", "--url", url], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, env=dict(os.environ, MOTTY_MCP_TOKEN=token), text=True)
bridge.stdin.write(json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "t", "version": "1"}}}) + "\n")
bridge.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
bridge.stdin.write(json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}) + "\n")
bridge.stdin.flush()
first = json.loads(bridge.stdout.readline())
second = json.loads(bridge.stdout.readline())
check("the stdio bridge initializes", first.get("result", {}).get("serverInfo", {}).get("name") == "motty", first)
check("the stdio bridge lists tools", len(second.get("result", {}).get("tools", [])) == 9, second)
bridge.stdin.close(); bridge.wait(timeout=5)
bad = subprocess.run(["/tmp/motty-bin", "mcp-bridge", "--url", url], env=dict(os.environ, MOTTY_MCP_TOKEN="x" * 30),
                     input=json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}) + "\n", capture_output=True, text=True, timeout=15)
check("the bridge with a wrong token does not get tools", "motty" not in bad.stdout or bad.returncode != 0, bad.stdout)

proc.stdin.write("quit\n"); proc.stdin.flush()
sys.exit(1 if failures else 0)
