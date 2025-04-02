# Spectre-Xenon Websocket

[![GoDoc](https://godoc.org/github.com/spectre-xenon/websocket?status.svg)](https://godoc.org/github.com/spectre-xenon/websocket)

A lightweight, efficient, and RFC 6455 compliant WebSocket implementation in Go.

## Documentation

- [API Reference](https://pkg.go.dev/github.com/spectre-xenon/websocket?tab=doc)
- [Echo example](https://github.com/spectre-xenon/websocket/tree/main/examples/echo)

## Features

**Fully Autobahn Testsuite Compliant**
_Passes all test cases from the [Autobahn Testsuite](https://github.com/crossbario/autobahn-testsuite) for strict protocol compliance_

- **100% RFC 6455 Compliance** - Verified by Autobahn Testsuite [^1]
- **Zero dependencies** - Only relies on the standard library
- **Dual Mode** - Client & Server implementations
- **RFC 7692 Per-Message DEFLATE** - Compression with context takeover support
- **Subprotocol Negotiation** - Easy protocol versioning
- **Clean API** - Simple `SendMessage()`/`NextMessage()` interface
- **JSON Helpers** - Built-in `SendJSON()`/`NextJSON()`
- **TLS Support** - Secure wss:// connections
- **Cookie Handling** - Integrated cookie jar for authentication

## Installation

```bash
go get github.com/spectre-xenon/websocket
```

## Contributing

1. Fork the repository
2. Create feature branch (`git checkout -b feature`)
3. Commit changes (`git commit -am 'Add feature'`)
4. make sure to run the test example in `examples/echo` with autobahn-testsuite
5. Push branch (`git push origin feature`)
6. Open Pull Request

[^1]: the only thing currently not supportted is setting the server_max_window_bits as this is not supported in the `compress/flate` package
