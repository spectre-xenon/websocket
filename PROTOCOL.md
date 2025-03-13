# What's this?

This document acts as a reference for the protocol implementation.
It's based on the offical [RFC6455](https://datatracker.ietf.org/doc/html/rfc6455)

# HandShake

### Client handshake headers

| Header                   |    Required/Optional    |        Value        | Purpose                                               |
| :----------------------- | :---------------------: | :-----------------: | :---------------------------------------------------- |
| Host                     |        Required         |         var         | Server's hostname and port.                           |
| Origin                   | Required (web browsers) |         var         | Client's origin for security and access control.      |
| Upgrade                  |        Required         |     "websocket"     | Singals upgrade to websocket protocol.                |
| Connection               |        Required         |      "Upgrade"      | Signals connection upgrade.                           |
| Sec-WebSocket-Key        |        Required         | var(base64-encoded) | Client's random key for security challange.           |
| Sec-WebSocket-Version    |        Required         |        "13"         | Client's desired WebSocket protocol version.          |
| Sec-WebSocket-Protocol   |        Optional         |         var         | Client's desired subprotocols. (if specified)         |
| Sec-WebSocket-Extensions |        Optional         |         var         | Client's desired WebSocket extensions. (if specified) |

### Server handshake headers

| Header                 | Required/Optional |    Value    | Purpose                                                |
| :--------------------- | :---------------: | :---------: | :----------------------------------------------------- |
| Upgrade                |     Required      | "websocket" | Singals upgrade to websocket protocol.                 |
| Connection             |     Required      |  "Upgrade"  | Signals connection upgrade.                            |
| Sec-WebSocket-Accept   |     Required      |     var     | Server's derived key, confirming successful handshake. |
| Sec-WebSocket-Version  |     Optional      |    "13"     | If the client sent a version the server is not using.  |
| Sec-WebSocket-Protocol |     Optional      |     var     | Server's desired subprotocols. (if specified)          |

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
- Decoded `Sec-WebSocket-Key` is not 16 bytes of random data

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
connection is in the `OPEN` state.

# Data Framing

### Base Framing Protocol

```
    0                   1                   2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
    +-+-+-+-+-------+-+-------------+-------------------------------+
    |F|R|R|R| opcode|M| Payload len |    Extended payload length    |
    |I|S|S|S|  (4)  |A|     (7)     |             (16/64)           |
    |N|V|V|V|       |S|             |   (if payload len==126/127)   |
    | |1|2|3|       |K|             |                               |
    +-+-+-+-+-------+-+-------------+ - - - - - - - - - - - - - - - +
    |     Extended payload length continued, if payload len == 127  |
    + - - - - - - - - - - - - - - - +-------------------------------+
    |                               |Masking-key, if MASK set to 1  |
    +-------------------------------+-------------------------------+
    | Masking-key (continued)       |          Payload Data         |
    +-------------------------------- - - - - - - - - - - - - - - - +
    :                     Payload Data continued ...                :
    + - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - +
    |                     Payload Data continued ...                |
    +---------------------------------------------------------------+
```

**FIN: 1 bit**

Indicates that this is the final fragment in a message. The first
fragment MAY also be the final fragment.

**RSV1, RSV2, RSV3: 1 bit each**

Reserved bits for extensions, MUST be 0 unless an extension is configured,
FAIL connection if otherwise.

**Opcode: 4 bits**

interpretation of the "Payload data", If an unknown
opcode is received, the receiving endpoint MUST FAIL the
WebSocket Connection.

- 0x0 denotes a continuation frame

- 0x1 denotes a text frame:

  - The "Payload data" is text data encoded as UTF-8.

- 0x2 denotes a binary frame:

  - The "Payload data" is arbitrary binary data whose interpretation
    is solely up to the application layer.

- 0x3-7 are reserved for further non-control frames

- 0x8 Close frame:

  - MAY contain a body that indicates a reason for closing

  - the first two bytes of
    the body MUST be a 2-byte unsigned integer (in network byte order)
    representing a status code

  - Following the 2-byte integer, the body MAY contain UTF-8-encoded data
    with value, the interpretation of which is not defined by
    this specification. This data is not necessarily human readable but
    may be useful for debugging or passing information relevant to the
    script that opened the connection. As the data is not guaranteed to
    be human readable, clients MUST NOT show it to end users.

  - The application MUST NOT send any more data frames after sending a
    Close frame.

  - If an endpoint receives a Close frame and did not previously send a
    Close frame, the endpoint MUST send a Close frame in response,
    where the endpoint typically echos the status code it received.

  - The server MUST close the underlying TCP
    connection immediately

  - the client SHOULD wait for the server to
    close the connection but MAY close the connection at any time after
    sending and receiving a Close message

- 0x9 Ping frame:

  - A Ping frame MAY include "Application data"

  - Upon receipt of a Ping frame, an endpoint MUST send a Pong frame in
    response, unless it already received a Close frame. It SHOULD
    respond with Pong frame as soon as is practical.

  - An endpoint MAY send a Ping frame any time after the connection is
    established and before the connection is closed.

- 0xA Pong frame:

  - A Pong frame sent in response to a Ping frame must have identical
    "Application data" as found in the message body of the Ping frame
    being replied to.

  - If an endpoint receives a Ping frame and has not yet sent Pong
    frame(s) in response to previous Ping frame(s), the endpoint MAY
    elect to send a Pong frame for only the most recently processed Ping
    frame.

  - A Pong frame MAY be sent unsolicited. This serves as a
    unidirectional heartbeat. A response to an unsolicited Pong frame is
    not expected.

- 0xB-F are reserved for further control frames yet to be defined

Defines whether the "Payload data" is masked. If set to 1, a
masking key is present in masking-key, and this is used to unmask
the "Payload data".

The client MUST mask all frames sent to server,
But the server MUST NOT mask any frame sent to client,
If either of these requirements is not met, the receiving endpoint MUST close the connection.
The receiver may also send a frame with status 1002

**Payload length: 7 bits, 7+16 bits, or 7+64 bits**

The length of the "Payload data", in bytes: if 0-125, that is the
payload length. If 126, the following 2 bytes interpreted as a
16-bit unsigned integer are the payload length. If 127, the
following 8 bytes interpreted as a 64-bit unsigned integer (the
most significant bit MUST be 0) are the payload length. Multibyte
length quantities are expressed in network byte order.

**Masking-key: 0 or 4 bytes**

This field is `32-bit` value
present if the mask bit is set to 1 and is absent if the mask bit
is set to 0.

**Extension data: x bytes**

The "Extension data" is 0 bytes unless an extension has been
negotiated.

**Application data: y bytes**

Arbitrary "Application data", taking up the remainder of the frame
after any "Extension data".

### Masking the data

uses XOR masking like follows for every byte in payload:

```go
masked-byte := payload[i] ^ masking-key[i % 4]
```

unmasking uses the same algorithm

### Fragmentation

The primary purpose of fragmentation is to allow sending a message
that is of unknown size when the message is started without having to
buffer that message.

The following rules apply to fragmentation:

- An unfragmented message consists of a single frame with the FIN
  bit set (Section 5.2) and an opcode other than 0.

- A fragmented message consists of a single frame with the FIN bit
  clear and an opcode other than 0, followed by zero or more frames
  with the FIN bit clear and the opcode set to 0, and terminated by
  a single frame with the FIN bit set and an opcode of 0.

  EXAMPLE: For a text message sent as three fragments, the first
  fragment would have an opcode of 0x1 and a FIN bit clear, the
  second fragment would have an opcode of 0x0 and a FIN bit clear,
  and the third fragment would have an opcode of 0x0 and a FIN bit
  that is set.

- Control frames MAY be injected in the middle of
  a fragmented message. Control frames themselves MUST NOT be
  fragmented

- Message fragments MUST be delivered to the recipient in the order
  sent by the sender.

- An endpoint MUST be capable of handling control frames in the
  middle of a fragmented message.

- A sender MAY create fragments of any size for non-control
  messages.

- Clients and servers MUST support receiving both fragmented and
  unfragmented messages.

- As a consequence of these rules, all fragments of a message are of
  the same type, as set by the first fragment's opcode. Since
  control frames cannot be fragmented, the type for all fragments in
  a message MUST be either text, binary, or one of the reserved
  opcodes.

# Security Considerations

### Origin Considerations

Servers that are not intended to process input from any web page but
only for certain sites MUST verify the |Origin| field is an origin
they expect. If the origin indicated is unacceptable to the server,
then it SHOULD respond to the WebSocket handshake with a reply
containing HTTP 403 Forbidden status code.

### Attacks On Infrastructure (Masking)

Masking is also used as a way to mitigate some types of attacks, specifically [proxy cache poisoning attacks](https://datatracker.ietf.org/doc/html/rfc6455#section-10.3)

# Status Codes

### 1000

indicates a normal closure, meaning that the purpose for
which the connection was established has been fulfilled.

### 1001

indicates that an endpoint is "going away", such as a server
going down or a browser having navigated away from a page.

### 1002

indicates that an endpoint is terminating the connection due
to a protocol error.

### 1003

indicates that an endpoint is terminating the connection
because it has received a type of data it cannot accept (e.g., an
endpoint that understands only text data MAY send this if it
receives a binary message).

### 1004

Reserved. The specific meaning might be defined in the future.

### 1005

is a reserved value and MUST NOT be set as a status code in a
Close control frame by an endpoint. It is designated for use in
applications expecting a status code to indicate that no status
code was actually present.

### 1006

is a reserved value and MUST NOT be set as a status code in a
Close control frame by an endpoint. It is designated for use in
applications expecting a status code to indicate that the
connection was closed abnormally, e.g., without sending or
receiving a Close control frame.

### 1007

indicates that an endpoint is terminating the connection
because it has received data within a message that was not
consistent with the type of the message (e.g., non-UTF-8 [RFC3629]
data within a text message).

### 1008

indicates that an endpoint is terminating the connection
because it has received a message that violates its policy. This
is a generic status code that can be returned when there is no
other more suitable status code (e.g., 1003 or 1009) or if there
is a need to hide specific details about the policy.

### 1009

indicates that an endpoint is terminating the connection
because it has received a message that is too big for it to
process.

### 1010

indicates that an endpoint (client) is terminating the
connection because it has expected the server to negotiate one or
more extension, but the server didn't return them in the response
message of the WebSocket handshake. The list of extensions that

### 1011

1011 indicates that a server is terminating the connection because
it encountered an unexpected condition that prevented it from
fulfilling the request.

### 1015

1015 is a reserved value and MUST NOT be set as a status code in a
Close control frame by an endpoint. It is designated for use in
applications expecting a status code to indicate that the
connection was closed due to a failure to perform a TLS handshake
(e.g., the server certificate can't be verified).
