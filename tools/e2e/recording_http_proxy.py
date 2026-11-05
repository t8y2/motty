#!/usr/bin/env python3
"""Record HTTP CONNECT targets while forwarding them to another local proxy."""

import argparse
import socketserver
import threading


def read_headers(stream):
    data = bytearray()
    while b"\r\n\r\n" not in data:
        chunk = stream.recv(4096)
        if not chunk:
            break
        data.extend(chunk)
        if len(data) > 64 << 10:
            raise ValueError("proxy request headers are too large")
    return bytes(data)


def relay(source, destination):
    try:
        while True:
            data = source.recv(64 << 10)
            if not data:
                break
            destination.sendall(data)
    except OSError:
        pass
    finally:
        try:
            destination.shutdown(1)
        except OSError:
            pass


class Handler(socketserver.BaseRequestHandler):
    def handle(self):
        client = self.request
        upstream = None
        try:
            request = read_headers(client)
            first_line = request.split(b"\r\n", 1)[0].decode("latin-1")
            if not first_line:
                return
            with self.server.log_lock:
                print(f"REQUEST {first_line}", file=self.server.log, flush=True)
            upstream = self.server.connection_factory()
            upstream.sendall(request)
            response = read_headers(upstream)
            status = response.split(b"\r\n", 1)[0].decode("latin-1")
            with self.server.log_lock:
                print(f"{first_line}\t{status}", file=self.server.log, flush=True)
            client.sendall(response)
            if not status.startswith("HTTP/1.1 200") and not status.startswith("HTTP/1.0 200"):
                upstream.close()
                return
            worker = threading.Thread(target=relay, args=(client, upstream), daemon=True)
            worker.start()
            relay(upstream, client)
        except (OSError, ValueError) as error:
            with self.server.log_lock:
                print(f"ERROR {error}", file=self.server.log, flush=True)
        finally:
            if upstream is not None:
                upstream.close()
            try:
                client.close()
            except OSError:
                pass


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, address, upstream_address, log):
        self.upstream_address = upstream_address
        self.log = log
        self.log_lock = threading.Lock()
        super().__init__(address, Handler)

    def connection_factory(self):
        return self.socket_factory(self.upstream_address)

    @staticmethod
    def socket_factory(address):
        import socket

        connection = socket.create_connection(address, timeout=12)
        connection.settimeout(None)
        return connection


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen-port", type=int, required=True)
    parser.add_argument("--upstream", required=True, help="HTTP proxy accepted by CONNECT, as HOST:PORT")
    parser.add_argument("--log", required=True)
    arguments = parser.parse_args()
    host, port = arguments.upstream.rsplit(":", 1)
    with open(arguments.log, "a", encoding="utf-8", buffering=1) as log:
        with Server(("127.0.0.1", arguments.listen_port), (host, int(port)), log) as server:
            print(f"recording proxy listening on 127.0.0.1:{arguments.listen_port}", flush=True)
            server.serve_forever()


if __name__ == "__main__":
    main()
