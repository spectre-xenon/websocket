# Echo

This's a server client echo example, which is also used to test aginst autobahn

## testing

1- Run the server with

```bash
go run server.go
```

### testing with client

2- run the client

```bash
go run client/client.go
```

### testing with autobahn

**prerequisites:** `go`, `docker`,`make`

2- test the server

```bash
make test-server
```
