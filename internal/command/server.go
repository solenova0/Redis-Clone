package command

import "github.com/solenova0/Redis-Clone/internal/protocol"

func registerServerCommands(d *Dispatcher) {
	d.register(Command{Name: "ping", Arity: -1, Handler: ping})
	d.register(Command{Name: "echo", Arity: 2, Handler: echo})
	d.register(Command{Name: "quit", Arity: -1, Handler: quit})
}

func ping(ctx *Context) protocol.Value {
	switch len(ctx.Args) {
	case 1:
		return protocol.Simple("PONG")
	case 2:
		return protocol.Bulk(ctx.Args[1])
	default:
		return protocol.Error("ERR wrong number of arguments for 'ping' command")
	}
}

func echo(ctx *Context) protocol.Value {
	return protocol.Bulk(ctx.Args[1])
}

func quit(ctx *Context) protocol.Value {
	ctx.Quit = true
	return protocol.OK()
}
