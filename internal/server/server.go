// Package server implements the TCP front end: it accepts client connections
// and runs one goroutine per client.
package server

import (
	"errors"
	"log/slog"
	"net"
	"sync"

	"github.com/solenova0/Redis-Clone/internal/command"
)

// ErrServerClosed is returned by Serve after Close has been called.
var ErrServerClosed = errors.New("server: closed")

// Server accepts TCP connections and serves each client in its own goroutine.
type Server struct {
	Addr       string
	Logger     *slog.Logger
	Dispatcher *command.Dispatcher

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	closed   bool
	wg       sync.WaitGroup
}

// New returns a Server that will listen on addr and execute requests with d.
func New(addr string, d *command.Dispatcher, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{Addr: addr, Logger: logger, Dispatcher: d}
}

// ListenAndServe listens on s.Addr and serves clients until Close is called.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve accepts connections on ln until Close is called. It takes ownership of ln.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		ln.Close()
		return ErrServerClosed
	}
	s.listener = ln
	s.conns = make(map[net.Conn]struct{})
	s.mu.Unlock()

	s.Logger.Info("listening", "addr", ln.Addr().String())

	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.isClosed() {
				return ErrServerClosed
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		if !s.track(conn) {
			conn.Close()
			return ErrServerClosed
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(conn)
			newClient(conn, s.Dispatcher, s.Logger).serve()
		}()
	}
}

// Close stops accepting, closes all client connections, and waits for their
// goroutines to finish.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
	return err
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	c.Close()
}
