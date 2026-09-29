package protocol

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func argsOf(ss ...string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}

func TestReadValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Value
	}{
		{"simple string", "+OK\r\n", Simple("OK")},
		{"error", "-ERR unknown command\r\n", Error("ERR unknown command")},
		{"integer", ":100\r\n", Integer(100)},
		{"negative integer", ":-42\r\n", Integer(-42)},
		{"bulk string", "$5\r\nhello\r\n", BulkString("hello")},
		{"empty bulk", "$0\r\n\r\n", BulkString("")},
		{"bulk with CRLF inside", "$4\r\na\r\nb\r\n", BulkString("a\r\nb")},
		{"null bulk", "$-1\r\n", NullBulk()},
		{"null array", "*-1\r\n", NullArray()},
		{"empty array", "*0\r\n", ArrayOf()},
		{"array", "*2\r\n$3\r\nGET\r\n$4\r\nname\r\n", ArrayOf(BulkString("GET"), BulkString("name"))},
		{"nested", "*2\r\n:1\r\n*1\r\n+x\r\n", ArrayOf(Integer(1), ArrayOf(Simple("x")))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewReader(strings.NewReader(tt.in)).ReadValue()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReadValueInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"unknown type", "?foo\r\n"},
		{"missing CR", "+OK\n"},
		{"bad integer", ":abc\r\n"},
		{"bad bulk length", "$abc\r\n"},
		{"negative bulk length", "$-2\r\n"},
		{"bulk too long", "$999999999999\r\n"},
		{"bulk missing CRLF", "$3\r\nabcXY"},
		{"bad array length", "*x\r\n"},
		{"array too long", "*99999999\r\n"},
		{"too deep", strings.Repeat("*1\r\n", MaxDepth+2) + ":1\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(tt.in)).ReadValue()
			var pe *ProtocolError
			if !errors.As(err, &pe) {
				t.Fatalf("got %v, want ProtocolError", err)
			}
		})
	}
}

func TestReadValueTruncated(t *testing.T) {
	for _, in := range []string{"+OK", "$5\r\nhel", "$5\r\nhello", "*2\r\n$3\r\nGET\r\n", "*2\r\n"} {
		_, err := NewReader(strings.NewReader(in)).ReadValue()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%q: got %v, want ErrUnexpectedEOF", in, err)
		}
	}
	if _, err := NewReader(strings.NewReader("")).ReadValue(); err != io.EOF {
		t.Errorf("empty input: got %v, want io.EOF", err)
	}
}

func TestReadCommandPipelined(t *testing.T) {
	in := "*1\r\n$4\r\nPING\r\n*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\nECHO hi\r\n*0\r\n\r\n*2\r\n$3\r\nGET\r\n$1\r\nk\r\n"
	want := [][][]byte{argsOf("PING"), argsOf("SET", "k", "v"), argsOf("ECHO", "hi"), argsOf("GET", "k")}

	// OneByteReader simulates the worst case of partial TCP reads.
	for name, src := range map[string]io.Reader{
		"whole":    strings.NewReader(in),
		"one byte": iotest.OneByteReader(strings.NewReader(in)),
	} {
		t.Run(name, func(t *testing.T) {
			r := NewReader(src)
			for i, w := range want {
				got, err := r.ReadCommand()
				if err != nil {
					t.Fatalf("command %d: %v", i, err)
				}
				if !reflect.DeepEqual(got, w) {
					t.Fatalf("command %d: got %q, want %q", i, got, w)
				}
			}
			if _, err := r.ReadCommand(); err != io.EOF {
				t.Fatalf("got %v, want io.EOF", err)
			}
		})
	}
}

func TestReadCommandInline(t *testing.T) {
	r := NewReader(strings.NewReader("  set  name   Solan \nPING\r\n"))
	got, err := r.ReadCommand()
	if err != nil || !reflect.DeepEqual(got, argsOf("set", "name", "Solan")) {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = r.ReadCommand()
	if err != nil || !reflect.DeepEqual(got, argsOf("PING")) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadCommandArgsDoNotAlias(t *testing.T) {
	r := NewReader(strings.NewReader("GET a\nGET b\n"))
	first, _ := r.ReadCommand()
	r.ReadCommand()
	if string(first[1]) != "a" {
		t.Fatalf("first command was overwritten: %q", first)
	}
}

func TestReadCommandLargeValue(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 1<<20)
	in := AppendCommand(nil, []byte("SET"), []byte("k"), big)
	got, err := NewReader(iotest.HalfReader(bytes.NewReader(in))).ReadCommand()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[2], big) {
		t.Fatal("large value mismatch")
	}
}

func TestReadCommandInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"non-bulk element", "*1\r\n:1\r\n"},
		{"null bulk element", "*1\r\n$-1\r\n"},
		{"malformed length", "*1\r\n$x\r\n"},
		{"missing CRLF after bulk", "*1\r\n$4\r\nPINGxx"},
		{"missing CR in header", "*1\n$4\r\nPING\r\n"},
		{"inline too long", strings.Repeat("a", MaxInlineLen+10) + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(tt.in)).ReadCommand()
			var pe *ProtocolError
			if !errors.As(err, &pe) {
				t.Fatalf("got %v, want ProtocolError", err)
			}
		})
	}
}

func TestReadCommandTruncated(t *testing.T) {
	for _, in := range []string{"*2\r\n$3\r\nGET\r\n", "*1\r\n$4\r\nPI", "*1", "PING"} {
		_, err := NewReader(strings.NewReader(in)).ReadCommand()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%q: got %v, want ErrUnexpectedEOF", in, err)
		}
	}
}

func TestAppendValue(t *testing.T) {
	tests := []struct {
		v    Value
		want string
	}{
		{OK(), "+OK\r\n"},
		{Error("ERR unknown command"), "-ERR unknown command\r\n"},
		{Error("ERR bad\r\nname"), "-ERR bad  name\r\n"},
		{Integer(100), ":100\r\n"},
		{BulkString("hello"), "$5\r\nhello\r\n"},
		{BulkString(""), "$0\r\n\r\n"},
		{NullBulk(), "$-1\r\n"},
		{NullArray(), "*-1\r\n"},
		{ArrayOf(BulkString("GET"), BulkString("name")), "*2\r\n$3\r\nGET\r\n$4\r\nname\r\n"},
	}
	for _, tt := range tests {
		if got := string(AppendValue(nil, tt.v)); got != tt.want {
			t.Errorf("AppendValue(%#v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	v := ArrayOf(Simple("OK"), Error("ERR x"), Integer(-7), BulkString("a\r\nb"), NullBulk(), ArrayOf(), NullArray())
	got, err := NewReader(bytes.NewReader(AppendValue(nil, v))).ReadValue()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, v) {
		t.Fatalf("got %#v, want %#v", got, v)
	}
}

func FuzzReadCommand(f *testing.F) {
	f.Add([]byte("*2\r\n$3\r\nGET\r\n$1\r\nk\r\n"))
	f.Add([]byte("PING\r\n"))
	f.Add([]byte("*1\r\n$-1\r\n"))
	f.Fuzz(func(t *testing.T, in []byte) {
		r := NewReader(bytes.NewReader(in))
		for range 100 {
			if _, err := r.ReadCommand(); err != nil {
				return
			}
		}
	})
}

func BenchmarkReadCommand(b *testing.B) {
	cmd := AppendCommand(nil, []byte("SET"), []byte("key:000123"), []byte("some-value-here"))
	in := bytes.Repeat(cmd, 1000)
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	for b.Loop() {
		r := NewReader(bytes.NewReader(in))
		for {
			if _, err := r.ReadCommand(); err != nil {
				break
			}
		}
	}
}
