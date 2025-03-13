# What's this?

This document acts as a reference for the protocol implementation

# HandShake

### Client handshake headers

| Header                   |    Required/Optional    |        Value        | Purpose                                                     |
| :----------------------- | :---------------------: | :-----------------: | :---------------------------------------------------------- |
| Host                     |        Required         |         var         | Server's hostname and port.                                 |
| Origin                   | Required (web browsers) |         var         | Client's origin for security and access control.            |
| Upgrade                  |        Required         |     "websocket"     | Singals upgrade to websocket protocol.                      |
| Connection               |        Required         |      "Upgrade"      | Signals connection upgrade.                                 |
| Sec-WebSocket-Key        |        Required         | var(base64-encoded) | Client's random key for security challange.                 |
| Sec-WebSocket-Version    |        Required         |        "13"         | Client's desired WebSocket protocol version.                |
| Sec-WebSocket-Protocol   |        Optional         |         var         | Client's desired WebSocket protocol version. (if specified) |
| Sec-WebSocket-Extensions |        Optional         |         var         | Client's desired WebSocket extensions. (if specified)       |

### Server handshake headers

| Header                   | Required/Optional |    Value    | Purpose                                                     |
| :----------------------- | :---------------: | :---------: | :---------------------------------------------------------- |
| Upgrade                  |     Required      | "websocket" | Singals upgrade to websocket protocol.                      |
| Connection               |     Required      |  "Upgrade"  | Signals connection upgrade.                                 |
| Sec-WebSocket-Accept     |     Required      |     var     | Server's derived key, confirming successful handshake.      |
| Sec-WebSocket-Version    |     Optional      |    "13"     | If the client sent a version the server is not using.       |
| Sec-WebSocket-Protocol   |     Optional      |     var     | Server's desired WebSocket protocol version. (if specified) |
| Sec-WebSocket-Extensions |     Optional      |     var     | Server's supported WebSocket extensions. (if specified)     |

### Flow

The handshake happens over http/https and uses http standard hearders
which can include any other headers not specified in the spec like `Cookies` for authentication.

The `Sec-WebSocket-Key` is a base64-encoded random generated key that's used in the security challange.
To prove that the handshake was received the server has to do the following:

- Get the `Sec-WebSocket-Key`.
- Concatenate the key with the GUID `58EAFA5-E914-47DA-95CA-C5AB0DC85B11` in string form.
- Generate a SHA-1 hash of the concatenated string.
- Base64-encode it.
- Place the result in the `Sec-WebSocket-Accept` response header.

Both `Sec-WebSocket-Protocol` and `Sec-WebSocket-Extensions` contain a list of protocol and extentsions
the client want to use respectively, and return the selected protocol and implemented extensions in the same response header.

The server/client must fail the connection if any of the follwing match:

**server:**

- Any method http other than `GET`
- Missing/Incorrect required headers value
- 16 bytes in length base64-encoded `Sec-WebSocket-Key`

**client:**

- Missing/Incorrect required headers value
- Incorrect `Sec-WebSocket-Accept` value
- Mismatched `Sec-WebSocket-Protocol` or `Sec-WebSocket-Extensions` value
  from what was sent
- A status code other than `101`

if the client's `Sec-WebSocket-Version` isn't supported by the server, the server
should fail the request and apped the same header with the supported version.

The response from the server uses a status code of `101` indicating success,
any other status code like `400` (Bad Request) indicates handshake failure

An example of the handshake from the client looks like this:

```
GET /ws HTTP/1.1
Host: server.example.com
Origin: http://example.com
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Protocol: chat, superchat
Sec-WebSocket-Version: 13
Sec-WebSocket-Extensions: permessage-deflate
```

a successful response from server look like this:

```
HTTP/1.1 101 Switching Protocols
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
Sec-WebSocket-Protocol: chat
Sec-WebSocket-Extensions: permessage-deflate
```

If the server finishes these
steps without aborting the WebSocket handshake, the server considers
the WebSocket connection to be established and that the WebSocket
connection is in the `OPEN` state[^1].

[^1]: The offical websocket [RFC6455](https://datatracker.ietf.org/doc/html/rfc6455)
