package command

import (
	"reflect"
	"strings"
	"testing"

	"github.com/solenova0/Redis-Clone/internal/protocol"
)

func args(s string) [][]byte {
	var out [][]byte
	for _, f := range strings.Fields(s) {
		out = append(out, []byte(f))
	}
	return out
}

func TestDispatch(t *testing.T) {
	d := NewDispatcher()
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
	got, _ := NewDispatcher().Dispatch(nil)
	if got.Type != protocol.TypeError {
		t.Fatalf("got %#v, want error", got)
	}
}
