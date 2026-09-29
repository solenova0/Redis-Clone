// Package protocol implements the Redis Serialization Protocol (RESP2).
package protocol

import "fmt"

// Type identifies a RESP value by its leading byte.
type Type byte

const (
	TypeSimpleString Type = '+'
	TypeError        Type = '-'
	TypeInteger      Type = ':'
	TypeBulkString   Type = '$'
	TypeArray        Type = '*'
)

// Limits applied to untrusted input so a client cannot make the server
// allocate unbounded memory from a single header.
const (
	MaxBulkLen   = 512 * 1024 * 1024
	MaxArrayLen  = 1024 * 1024
	MaxInlineLen = 64 * 1024
	MaxDepth     = 32
)

// Value is a decoded RESP value. Which field is meaningful depends on Type;
// Null marks a null bulk string ($-1) or null array (*-1).
type Value struct {
	Type  Type
	Str   string
	Int   int64
	Bulk  []byte
	Array []Value
	Null  bool
}

func Simple(s string) Value     { return Value{Type: TypeSimpleString, Str: s} }
func Error(msg string) Value    { return Value{Type: TypeError, Str: msg} }
func Integer(n int64) Value     { return Value{Type: TypeInteger, Int: n} }
func Bulk(b []byte) Value       { return Value{Type: TypeBulkString, Bulk: b} }
func BulkString(s string) Value { return Value{Type: TypeBulkString, Bulk: []byte(s)} }
func NullBulk() Value           { return Value{Type: TypeBulkString, Null: true} }
func ArrayOf(vs ...Value) Value {
	if vs == nil {
		vs = []Value{}
	}
	return Value{Type: TypeArray, Array: vs}
}
func NullArray() Value                     { return Value{Type: TypeArray, Null: true} }
func OK() Value                            { return Simple("OK") }
func Errorf(format string, a ...any) Value { return Error(fmt.Sprintf(format, a...)) }

// ProtocolError reports malformed RESP input. The connection cannot be
// resynchronised after one, so servers should reply and then disconnect.
type ProtocolError struct{ msg string }

func (e *ProtocolError) Error() string { return "Protocol error: " + e.msg }

func protoErr(format string, a ...any) error {
	return &ProtocolError{msg: fmt.Sprintf(format, a...)}
}
