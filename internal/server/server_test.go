package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/solenova0/Redis-Clone/internal/command"
	"github.com/solenova0/Redis-Clone/internal/protocol"
	"github.com/solenova0/Redis-Clone/internal/store"
)

func startServer(t *testing.T) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("", command.NewDispatcher(store.New()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		srv.Close()
		if err := <-done; !errors.Is(err, ErrServerClosed) {
			t.Errorf("Serve returned %v, want ErrServerClosed", err)
		}
	})
	return srv, ln.Addr().String()
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func cmd(args ...string) []byte {
	bs := make([][]byte, len(args))
	for i, a := range args {
		bs[i] = []byte(a)
	}
	return protocol.AppendCommand(nil, bs...)
}

func expect(t *testing.T, r *protocol.Reader, want protocol.Value) {
	t.Helper()
	got, err := r.ReadValue()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestPingEcho(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	conn.Write(cmd("PING"))
	expect(t, r, protocol.Simple("PONG"))
	conn.Write(cmd("ECHO", "hello world"))
	expect(t, r, protocol.BulkString("hello world"))
	conn.Write([]byte("PING inline\n"))
	expect(t, r, protocol.BulkString("inline"))
	conn.Write(cmd("FOO"))
	expect(t, r, protocol.Error("ERR unknown command 'FOO', with args beginning with:"))
}

func TestStringCommands(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	steps := []struct {
		req  []byte
		want protocol.Value
	}{
		{cmd("GET", "name"), protocol.NullBulk()},
		{cmd("SET", "name", "Solan"), protocol.OK()},
		{cmd("GET", "name"), protocol.BulkString("Solan")},
		{[]byte("set age 22\r\n"), protocol.OK()},
		{cmd("EXISTS", "name", "age", "nope"), protocol.Integer(2)},
		{cmd("DEL", "name", "nope"), protocol.Integer(1)},
		{cmd("GET", "name"), protocol.NullBulk()},
		{cmd("SET", "k"), protocol.Error("ERR wrong number of arguments for 'set' command")},
		{cmd("SET", "k", "v", "EX", "10"), protocol.Error("ERR syntax error")},
	}
	for _, s := range steps {
		conn.Write(s.req)
		expect(t, r, s.want)
	}
}

func TestClientsShareKeyspace(t *testing.T) {
	_, addr := startServer(t)
	a := dial(t, addr)
	b := dial(t, addr)

	a.Write(cmd("SET", "shared", "from-a"))
	expect(t, protocol.NewReader(a), protocol.OK())
	b.Write(cmd("GET", "shared"))
	expect(t, protocol.NewReader(b), protocol.BulkString("from-a"))
}

func TestPipelinedAndSplitWrites(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	var buf []byte
	for i := range 100 {
		buf = append(buf, cmd("ECHO", fmt.Sprint(i))...)
	}
	// Deliver in odd-sized fragments so commands straddle TCP reads.
	go func() {
		for len(buf) > 0 {
			n := min(len(buf), 7)
			conn.Write(buf[:n])
			buf = buf[n:]
		}
	}()
	for i := range 100 {
		expect(t, r, protocol.BulkString(fmt.Sprint(i)))
	}
}

func TestCompleteCommandFollowedByPartial(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	ping := cmd("PING")
	conn.Write(append(cmd("ECHO", "a"), ping[:5]...))
	expect(t, r, protocol.BulkString("a"))
	conn.Write(ping[5:])
	expect(t, r, protocol.Simple("PONG"))
}

func TestLargeValue(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	big := string(bytes.Repeat([]byte("abcdefgh"), 256*1024))
	go conn.Write(cmd("ECHO", big))
	expect(t, r, protocol.BulkString(big))
}

func TestProtocolErrorClosesConnection(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	conn.Write([]byte("*1\r\n$x\r\n"))
	v, err := r.ReadValue()
	if err != nil || v.Type != protocol.TypeError {
		t.Fatalf("got %#v, %v; want error reply", v, err)
	}
	if _, err := r.ReadValue(); err != io.EOF {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func TestQuit(t *testing.T) {
	_, addr := startServer(t)
	conn := dial(t, addr)
	r := protocol.NewReader(conn)

	conn.Write(append(cmd("QUIT"), cmd("PING")...))
	expect(t, r, protocol.OK())
	if _, err := r.ReadValue(); err != io.EOF {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func TestConcurrentClients(t *testing.T) {
	_, addr := startServer(t)

	const clients = 50
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			r := protocol.NewReader(conn)
			for j := range 20 {
				msg := fmt.Sprintf("client-%d-msg-%d", i, j)
				if _, err := conn.Write(cmd("ECHO", msg)); err != nil {
					errs <- err
					return
				}
				got, err := r.ReadValue()
				if err != nil {
					errs <- err
					return
				}
				if string(got.Bulk) != msg {
					errs <- fmt.Errorf("got %q, want %q", got.Bulk, msg)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCloseDisconnectsClients(t *testing.T) {
	srv, addr := startServer(t)
	conn := dial(t, addr)

	// Round-trip once so the server has registered the connection.
	conn.Write(cmd("PING"))
	expect(t, protocol.NewReader(conn), protocol.Simple("PONG"))

	srv.Close()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected read error after server close")
	}
}

func TestServeAfterClose(t *testing.T) {
	srv := New("", command.NewDispatcher(store.New()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(ln); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("got %v, want ErrServerClosed", err)
	}
}
