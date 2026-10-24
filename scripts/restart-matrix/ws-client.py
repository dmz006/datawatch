#!/usr/bin/env python3
# scripts/restart-matrix/ws-client.py — WebSocket client for the restart matrix.
#
# Holds one connection to the sandbox daemon's /ws endpoint and appends one JSON
# line per event to --out: connect, message, disconnect, reconnected (with
# down_for, the seconds from the drop to the new connection) and gave_up. When the
# connection drops it reconnects with a short backoff, and it stops once the
# --stop file exists or after --give-up-after seconds of failed reconnects.
#
# Standard library only. Run it isolated:
#   python3 -I scripts/restart-matrix/ws-client.py --url ws://127.0.0.1:PORT/ws \
#       --name one --out FILE --stop FILE [--give-up-after SECONDS]

import argparse
import base64
import json
import os
import secrets
import select
import socket
import struct
import time
from urllib.parse import urlparse


def emit(path, ev, **fields):
    fields.update(ev=ev, t=round(time.time(), 3))
    with open(path, "a") as f:
        f.write(json.dumps(fields) + "\n")


def recv_exact(sock, n):
    buf = bytearray()
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise ConnectionError("peer closed")
        buf.extend(chunk)
    return bytes(buf)


def handshake(host, port, path):
    sock = socket.create_connection((host, port), timeout=5)
    key = base64.b64encode(secrets.token_bytes(16)).decode()
    request = (
        f"GET {path} HTTP/1.1\r\n"
        f"Host: {host}:{port}\r\n"
        "Upgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\n"
        "Sec-WebSocket-Version: 13\r\n\r\n"
    )
    sock.sendall(request.encode())
    head = bytearray()
    while not head.endswith(b"\r\n\r\n"):
        byte = sock.recv(1)
        if not byte:
            sock.close()
            raise ConnectionError("peer closed during handshake")
        head.extend(byte)
        if len(head) > 8192:
            sock.close()
            raise ConnectionError("handshake response too long")
    status = bytes(head).split(b"\r\n", 1)[0].decode(errors="replace")
    if " 101" not in status:
        sock.close()
        raise ConnectionError(f"handshake refused: {status}")
    sock.settimeout(None)
    return sock


def send_frame(sock, opcode, payload=b""):
    # Client frames are masked (RFC 6455 section 5.3).
    mask = secrets.token_bytes(4)
    n = len(payload)
    if n < 126:
        header = bytes([0x80 | opcode, 0x80 | n])
    elif n < 65536:
        header = bytes([0x80 | opcode, 0x80 | 126]) + struct.pack(">H", n)
    else:
        header = bytes([0x80 | opcode, 0x80 | 127]) + struct.pack(">Q", n)
    masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    sock.sendall(header + mask + masked)


def read_message(sock):
    """Reads one data message, answering pings on the way.

    Returns (opcode, payload); opcode 8 means the server closed the connection.
    Raises ConnectionError when the socket ends mid-message.
    """
    payload = bytearray()
    message_opcode = None
    while True:
        head = recv_exact(sock, 2)
        fin = head[0] & 0x80
        opcode = head[0] & 0x0F
        length = head[1] & 0x7F
        if length == 126:
            length = struct.unpack(">H", recv_exact(sock, 2))[0]
        elif length == 127:
            length = struct.unpack(">Q", recv_exact(sock, 8))[0]
        mask = recv_exact(sock, 4) if head[1] & 0x80 else None
        data = recv_exact(sock, length)
        if mask:
            data = bytes(b ^ mask[i % 4] for i, b in enumerate(data))
        if opcode == 9:
            send_frame(sock, 10, data)
            continue
        if opcode == 10:
            continue
        if opcode == 8:
            return 8, b""
        if opcode != 0:
            message_opcode = opcode
            payload = bytearray()
        payload.extend(data)
        if fin:
            return message_opcode, bytes(payload)


def message_type(payload):
    try:
        obj = json.loads(payload.decode("utf-8", "replace"))
    except ValueError:
        return "non-json"
    return obj.get("type", "") if isinstance(obj, dict) else "array"


def main():
    ap = argparse.ArgumentParser(description="restart-matrix WebSocket client")
    ap.add_argument("--url", required=True)
    ap.add_argument("--name", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--stop", required=True)
    ap.add_argument("--give-up-after", type=float, default=90.0)
    args = ap.parse_args()

    url = urlparse(args.url)
    host, port, path = url.hostname, url.port, url.path or "/"
    dropped_at = None
    attempt = 0
    while not os.path.exists(args.stop):
        try:
            sock = handshake(host, port, path)
        except OSError as err:
            if dropped_at is None:
                dropped_at = time.time()
            if time.time() - dropped_at > args.give_up_after:
                emit(args.out, "gave_up", client=args.name, error=str(err))
                return 1
            time.sleep(min(0.2 * (2**attempt), 2.0))
            attempt += 1
            continue

        attempt = 0
        emit(args.out, "connect", client=args.name)
        if dropped_at is not None:
            emit(args.out, "reconnected", client=args.name, down_for=round(time.time() - dropped_at, 3))
            dropped_at = None
        try:
            while not os.path.exists(args.stop):
                ready, _, _ = select.select([sock], [], [], 1.0)
                if not ready:
                    continue
                opcode, payload = read_message(sock)
                if opcode == 8:
                    break
                emit(args.out, "message", client=args.name, bytes=len(payload), type=message_type(payload))
        except OSError:
            pass
        finally:
            sock.close()
        if os.path.exists(args.stop):
            break
        emit(args.out, "disconnect", client=args.name)
        dropped_at = time.time()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
