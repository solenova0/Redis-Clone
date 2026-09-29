package command

import (
	"reflect"
	"strings"
	"testing"

	"github.com/solenova0/Redis-Clone/internal/protocol"
	"github.com/solenova0/Redis-Clone/internal/store"
)

func args(s string) [][]byte {
	var out [][]byte
	for _, f := range strings.Fields(s) {
		out = append(out, []byte(f))
	}
	return out
}

func TestDispatch(t *testing.T) {
	d := NewDispatcher(store.New())
	tests := []struct {
		in   string
		want protocol.Value
		quit bool
	}{
		{"PING", protocol.Simple("PONG"), false},
		{"ping", protocol.Simple("PONG"), false},
		{"PiNg hello", protocol.BulkString("hello"), false},
		{"PING a b", protocol.Error("ERR wrong number of arguments for 'ping' command"), false},
		{"ECHO hi", protocol.BulkString("hi"), false},
		{"ECHO", protocol.Error("ERR wrong number of arguments for 'echo' command"), false},
		{"ECHO a b", protocol.Error("ERR wrong number of arguments for 'echo' command"), false},
		{"QUIT", protocol.OK(), true},
		{"NOPE a b c d", protocol.Error("ERR unknown command 'NOPE', with args beginning with: 'a' 'b' 'c'"), false},
		{"NOPE", protocol.Error("ERR unknown command 'NOPE', with args beginning with:"), false},
	}
	for _, tt := range tests {
		got, quit := d.Dispatch(args(tt.in))
		if !reflect.DeepEqual(got, tt.want) || quit != tt.quit {
			t.Errorf("%q: got (%#v, %v), want (%#v, %v)", tt.in, got, quit, tt.want, tt.quit)
		}
	}
}

func TestDispatchEmpty(t *testing.T) {
	got, _ := NewDispatcher(store.New()).Dispatch(nil)
	if got.Type != protocol.TypeError {
		t.Fatalf("got %#v, want error", got)
	}
}

func TestStringAndKeyCommands(t *testing.T) {
	d := NewDispatcher(store.New())
	wrongArgs := func(name string) protocol.Value {
		return protocol.Error("ERR wrong number of arguments for '" + name + "' command")
	}
	// Steps run in order against one store.
	steps := []struct {
		in   string
		want protocol.Value
	}{
		{"GET k", protocol.NullBulk()},
		{"SET k v1", protocol.OK()},
		{"GET k", protocol.BulkString("v1")},
		{"SET k v2", protocol.OK()},
		{"get k", protocol.BulkString("v2")},
		{"SET a 1", protocol.OK()},
		{"EXISTS k a missing k", protocol.Integer(3)},
		{"DEL k missing", protocol.Integer(1)},
		{"DEL k", protocol.Integer(0)},
		{"EXISTS k", protocol.Integer(0)},
		{"SET k v x", protocol.Error("ERR syntax error")},

		{"SET", wrongArgs("set")},
		{"SET k", wrongArgs("set")},
		{"GET", wrongArgs("get")},
		{"GET a b", wrongArgs("get")},
		{"DEL", wrongArgs("del")},
		{"EXISTS", wrongArgs("exists")},
	}
	for _, s := range steps {
		got, _ := d.Dispatch(args(s.in))
		if !reflect.DeepEqual(got, s.want) {
			t.Errorf("%q: got %#v, want %#v", s.in, got, s.want)
		}
	}
}
