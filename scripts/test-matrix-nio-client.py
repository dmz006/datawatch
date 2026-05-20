#!/usr/bin/env python3
"""
BL241 P1 — Matrix nio test peer.

Invoked by test-matrix-synapse.sh via the nio-client Docker container.
Registers (or logs in) as @niotestuser:localhost, joins the test room,
sends a test message, and writes results to /tmp/nio-result.json.

Exit 0 = sent OK; exit 1 = error.
"""
import asyncio
import json
import os
import sys
import time

try:
    import nio
except ImportError:
    print("matrix-nio not installed — run: pip install matrix-nio", file=sys.stderr)
    sys.exit(1)

SYNAPSE_URL = os.environ.get("SYNAPSE_URL", "http://localhost:8008")
NIO_USER_ID = os.environ.get("NIO_USER", "@niotestuser:localhost")
NIO_PASSWORD = os.environ.get("NIO_PASSWORD", "niotestpassword")
NIO_ROOM = os.environ.get("NIO_ROOM", "#dw-test:localhost")
RESULT_FILE = "/tmp/nio-result.json"


async def main() -> int:
    localpart = NIO_USER_ID.lstrip("@").split(":")[0]

    client = nio.AsyncClient(SYNAPSE_URL, NIO_USER_ID)

    # Register (ignores M_USER_IN_USE — already registered on retry)
    try:
        reg = await client.register(localpart, NIO_PASSWORD)
        if isinstance(reg, nio.RegisterError):
            if "M_USER_IN_USE" not in str(reg):
                print(f"Register error: {reg}", file=sys.stderr)
                # Fall through to login
    except Exception:
        pass

    # Login
    login = await client.login(NIO_PASSWORD)
    if isinstance(login, nio.LoginError):
        print(f"Login failed: {login}", file=sys.stderr)
        await client.close()
        return 1

    # Join room
    join = await client.join(NIO_ROOM)
    if isinstance(join, nio.JoinError):
        print(f"Join error: {join}", file=sys.stderr)
        await client.close()
        return 1

    room_id = join.room_id if hasattr(join, "room_id") else NIO_ROOM

    # Send test message
    ts = int(time.time())
    msg = f"datawatch-matrix-nio-test-{ts}"
    send = await client.room_send(
        room_id,
        message_type="m.room.message",
        content={"msgtype": "m.text", "body": msg},
    )
    if isinstance(send, nio.RoomSendError):
        print(f"Send error: {send}", file=sys.stderr)
        await client.close()
        return 1

    event_id = send.event_id if hasattr(send, "event_id") else ""

    result = {
        "ok": True,
        "user_id": NIO_USER_ID,
        "room_id": room_id,
        "event_id": event_id,
        "message": msg,
        "timestamp": ts,
    }
    with open(RESULT_FILE, "w") as f:
        json.dump(result, f)
    print(json.dumps(result, indent=2))

    await client.close()
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
