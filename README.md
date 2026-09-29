# Redis Clone in Go

A Redis-compatible in-memory key-value server written from scratch in Go, built to learn how Redis works internally: the TCP front end, the RESP wire protocol, command dispatch, and (in later phases) storage, expiration, and AOF persistence.

It uses only the Go standard library.

## Status

| Phase | Scope | State |
|-------|-------|-------|
| 1 | TCP server, one goroutine per client, graceful shutdown | Done |
| 2 | RESP parser/encoder, command dispatcher, `PING` / `ECHO` / `QUIT` | Done |
| 3 | In-memory store: `SET` `GET` `DEL` `EXISTS` | Next |
| 4–9 | More commands, TTL, AOF, benchmarks, Docker, config | Planned |

## Architecture

```mermaid
flowchart LR
    Client -->|TCP| Conn[server: client goroutine]
    Conn --> Parser[protocol: RESP reader]
    Parser --> Dispatcher[command: dispatcher]
    Dispatcher --> Handler[command handler]
    Handler --> Encoder[protocol: RESP encoder]
    Encoder -->|buffered write| Client
```

```
cmd/server/          entry point: flags, logging, signal handling
internal/server/     TCP listener, per-connection loop, shutdown
internal/protocol/   RESP types, streaming parser, encoder
internal/command/    dispatcher and command handlers
```

## RESP implementation

- `protocol.Reader` wraps the connection in a `bufio.Reader`, so a command split across many TCP reads, or several commands in one read, are both handled naturally. No code assumes one `Read()` equals one command.
- Requests are RESP arrays of bulk strings; plain-text inline commands (`PING`, `ECHO hi`) are also accepted, so `nc` works.
- Input is untrusted. Bulk length (512 MiB), array length (1M), inline line length (64 KiB), and nesting depth are all capped. Large bulk payloads grow as bytes arrive instead of being allocated up front from the declared length. Missing CRLF, bad lengths, and unknown type bytes return a `ProtocolError`; the server replies `-ERR Protocol error: ...` and closes the connection, as Redis does. The parser is fuzz-tested.
- Replies are written to a `bufio.Writer` that is flushed only right before the reader blocks on the socket. This way replies to pipelined commands go out in one write, and a trailing partial command never holds back replies to complete ones.

## Concurrency model

Each accepted connection gets its own goroutine. The server tracks live connections so `Close()` (triggered by SIGINT/SIGTERM) stops the listener, closes every client, and waits for their goroutines to exit. The dispatcher is read-only after construction, so it is shared without locks.

## Commands

| Command | Notes |
|---------|-------|
| `PING [message]` | `PONG`, or echoes `message` |
| `ECHO message` | |
| `QUIT` | replies `OK`, then closes the connection |

Arity is checked centrally, Redis-style (positive = exact, negative = minimum). Errors match Redis wording, e.g. `ERR wrong number of arguments for 'echo' command`.

## Running

Requires Go 1.26+.

```sh
make run              # go run ./cmd/server (listens on localhost:6379)
make build            # bin/redis-clone
./bin/redis-clone -addr :6380
```

Try it:

```sh
redis-cli -p 6379 PING
# or without redis-cli:
printf 'PING\r\nECHO hello\r\n' | nc localhost 6379
```

## Testing

```sh
make test             # go test ./...
make race             # go test -race ./...
make benchmark
go test -fuzz FuzzReadCommand -fuzztime 30s ./internal/protocol
```

The tests cover RESP parsing (all types, invalid input, truncated input, byte-at-a-time reads, large values), encoding round trips, dispatcher arity and errors, and end-to-end TCP behavior: pipelining, fragmented writes, protocol errors, `QUIT`, 50 concurrent clients, and shutdown.
