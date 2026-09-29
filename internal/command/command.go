// Package command maps client requests to handlers and produces RESP replies.
package command

import (
	"fmt"
	"strings"

	"github.com/solenova0/Redis-Clone/internal/protocol"
)

// Context carries per-request state into a handler.
type Context struct {
	Args [][]byte // Args[0] is the command name.
	// Quit is set by a handler to ask the connection to close after replying.
	Quit bool
}

// Handler executes a command. Argument counts are validated before it runs.
type Handler func(ctx *Context) protocol.Value

// Command describes one command. Arity follows Redis: a positive value is an
// exact argument count (including the name), a negative value is a minimum.
type Command struct {
	Name    string
	Arity   int
	Handler Handler
}

// Dispatcher routes requests to commands. It is read-only after construction
// and therefore safe for concurrent use.
type Dispatcher struct {
	commands map[string]*Command
}

func NewDispatcher() *Dispatcher {
	d := &Dispatcher{commands: make(map[string]*Command)}
	registerServerCommands(d)
	return d
}

func (d *Dispatcher) register(c Command) {
	d.commands[c.Name] = &c
}

// Dispatch runs args and returns the reply and whether the connection should close.
func (d *Dispatcher) Dispatch(args [][]byte) (protocol.Value, bool) {
	if len(args) == 0 {
		return protocol.Error("ERR empty command"), false
	}
	name := strings.ToLower(string(args[0]))
	cmd, ok := d.commands[name]
	if !ok {
		return unknownCommand(args), false
	}
	if (cmd.Arity > 0 && len(args) != cmd.Arity) || (cmd.Arity < 0 && len(args) < -cmd.Arity) {
		return protocol.Errorf("ERR wrong number of arguments for '%s' command", cmd.Name), false
	}
	ctx := &Context{Args: args}
	reply := cmd.Handler(ctx)
	return reply, ctx.Quit
}

func unknownCommand(args [][]byte) protocol.Value {
	var b strings.Builder
	fmt.Fprintf(&b, "ERR unknown command '%s', with args beginning with:", truncate(args[0]))
	for _, a := range args[1:min(len(args), 4)] {
		fmt.Fprintf(&b, " '%s'", truncate(a))
	}
	return protocol.Error(b.String())
}

func truncate(b []byte) string {
	const max = 128
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
