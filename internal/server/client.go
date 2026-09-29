package server

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"

	"github.com/solenova0/Redis-Clone/internal/command"
	"github.com/solenova0/Redis-Clone/internal/protocol"
)

// client serves a single TCP connection: read request, dispatch, write reply.
type client struct {
	conn       net.Conn
	dispatcher *command.Dispatcher
	logger     *slog.Logger
	w          *bufio.Writer
	r          *protocol.Reader
}

func newClient(conn net.Conn, d *command.Dispatcher, logger *slog.Logger) *client {
	c := &client{
		conn:       conn,
		dispatcher: d,
		logger:     logger.With("remote", conn.RemoteAddr().String()),
		w:          bufio.NewWriterSize(conn, 16*1024),
	}
	c.r = protocol.NewReader(flushBeforeRead{conn: conn, w: c.w})
	return c
}

// flushBeforeRead flushes pending replies only when the reader must block on
// the socket, so replies to pipelined commands are batched into one write.
type flushBeforeRead struct {
	conn net.Conn
	w    *bufio.Writer
}

func (f flushBeforeRead) Read(p []byte) (int, error) {
	if f.w.Buffered() > 0 {
		if err := f.w.Flush(); err != nil {
			return 0, err
		}
	}
	return f.conn.Read(p)
}

func (c *client) serve() {
	c.logger.Debug("client connected")
	defer c.logger.Debug("client disconnected")

	var out []byte
	for {
		args, err := c.r.ReadCommand()
		if err != nil {
			var pe *protocol.ProtocolError
			if errors.As(err, &pe) {
				c.w.Write(protocol.AppendValue(nil, protocol.Error("ERR "+pe.Error())))
				c.w.Flush()
			} else if !errors.Is(err, io.EOF) {
				c.logError("read", err)
			}
			return
		}

		reply, quit := c.dispatcher.Dispatch(args)
		out = protocol.AppendValue(out[:0], reply)
		if _, err := c.w.Write(out); err != nil {
			c.logError("write", err)
			return
		}
		if cap(out) > 64*1024 {
			out = nil // don't pin one large reply's buffer for the connection's lifetime
		}
		if quit {
			c.w.Flush()
			return
		}
	}
}

func (c *client) logError(op string, err error) {
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrUnexpectedEOF) {
		return
	}
	c.logger.Warn("connection error", "op", op, "err", err)
}
