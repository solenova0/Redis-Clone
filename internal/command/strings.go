package command

import (
	"github.com/solenova0/Redis-Clone/internal/protocol"
)

func registerStringCommands(d *Dispatcher) {
	d.register(Command{Name: "set", Arity: -3, Handler: set})
	d.register(Command{Name: "get", Arity: 2, Handler: get})
}

// SET key value. Options (EX, PX, NX, XX, ...) are not supported yet.
func set(ctx *Context) protocol.Value {
	if len(ctx.Args) > 3 {
		return protocol.Error("ERR syntax error")
	}
	ctx.Store.Set(string(ctx.Args[1]), ctx.Args[2])
	return protocol.OK()
}

func get(ctx *Context) protocol.Value {
	val, ok, err := ctx.Store.Get(string(ctx.Args[1]))
	if err != nil {
		return protocol.Error(err.Error())
	}
	if !ok {
		return protocol.NullBulk()
	}
	return protocol.Bulk(val)
}
