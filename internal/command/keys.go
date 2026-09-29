package command

import "github.com/solenova0/Redis-Clone/internal/protocol"

func registerKeyCommands(d *Dispatcher) {
	d.register(Command{Name: "del", Arity: -2, Handler: del})
	d.register(Command{Name: "exists", Arity: -2, Handler: exists})
}

func del(ctx *Context) protocol.Value {
	return protocol.Integer(int64(ctx.Store.Delete(keys(ctx.Args[1:])...)))
}

func exists(ctx *Context) protocol.Value {
	return protocol.Integer(int64(ctx.Store.Exists(keys(ctx.Args[1:])...)))
}

func keys(args [][]byte) []string {
	ks := make([]string, len(args))
	for i, a := range args {
		ks[i] = string(a)
	}
	return ks
}
