package protocol

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
)

// Reader decodes RESP values from a byte stream. It relies on bufio to
// handle partial TCP reads and multiple pipelined commands per read.
type Reader struct {
	br *bufio.Reader
}

func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 16*1024)}
}

// ReadCommand reads the next client request and returns its arguments.
// Requests are either RESP arrays of bulk strings or inline commands
// (space-separated text lines, as typed into nc/telnet). Empty requests are
// skipped. It returns io.EOF only on a clean end of stream between commands.
// The returned argument slices are freshly allocated and owned by the caller.
func (r *Reader) ReadCommand() ([][]byte, error) {
	for {
		b, err := r.br.Peek(1)
		if err != nil {
			return nil, err
		}
		var args [][]byte
		if b[0] == byte(TypeArray) {
			args, err = r.readMultiBulk()
		} else {
			args, err = r.readInline()
		}
		if err != nil || len(args) > 0 {
			return args, err
		}
	}
}

func (r *Reader) readMultiBulk() ([][]byte, error) {
	r.br.ReadByte() // '*', already peeked
	n, err := r.readLength(MaxArrayLen, "multibulk")
	if err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, nil
	}
	args := make([][]byte, 0, min(n, 1024))
	for range n {
		t, err := r.br.ReadByte()
		if err != nil {
			return nil, unexpectedEOF(err)
		}
		if t != byte(TypeBulkString) {
			return nil, protoErr("expected '$', got '%c'", t)
		}
		l, err := r.readLength(MaxBulkLen, "bulk")
		if err != nil {
			return nil, err
		}
		if l < 0 {
			return nil, protoErr("invalid bulk length")
		}
		data, err := r.readBulk(l)
		if err != nil {
			return nil, err
		}
		args = append(args, data)
	}
	return args, nil
}

func (r *Reader) readInline() ([][]byte, error) {
	line, err := r.readLine(MaxInlineLen, false)
	if err != nil {
		return nil, err
	}
	fields := bytes.Fields(line)
	args := make([][]byte, len(fields))
	for i, f := range fields {
		args[i] = bytes.Clone(f)
	}
	return args, nil
}

// ReadValue reads any RESP value. It is used for replies and AOF replay.
func (r *Reader) ReadValue() (Value, error) {
	return r.readValue(0)
}

func (r *Reader) readValue(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, protoErr("nesting too deep")
	}
	t, err := r.br.ReadByte()
	if err != nil {
		if depth > 0 {
			err = unexpectedEOF(err)
		}
		return Value{}, err
	}
	switch Type(t) {
	case TypeSimpleString, TypeError:
		line, err := r.readLine(MaxInlineLen, true)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: Type(t), Str: string(line)}, nil
	case TypeInteger:
		line, err := r.readLine(MaxInlineLen, true)
		if err != nil {
			return Value{}, err
		}
		n, err := strconv.ParseInt(string(line), 10, 64)
		if err != nil {
			return Value{}, protoErr("invalid integer")
		}
		return Integer(n), nil
	case TypeBulkString:
		n, err := r.readLength(MaxBulkLen, "bulk")
		if err != nil {
			return Value{}, err
		}
		if n < 0 {
			return NullBulk(), nil
		}
		data, err := r.readBulk(n)
		if err != nil {
			return Value{}, err
		}
		return Bulk(data), nil
	case TypeArray:
		n, err := r.readLength(MaxArrayLen, "multibulk")
		if err != nil {
			return Value{}, err
		}
		if n < 0 {
			return NullArray(), nil
		}
		vs := make([]Value, 0, min(n, 1024))
		for range n {
			v, err := r.readValue(depth + 1)
			if err != nil {
				return Value{}, unexpectedEOF(err)
			}
			vs = append(vs, v)
		}
		return ArrayOf(vs...), nil
	default:
		return Value{}, protoErr("unknown type byte '%c'", t)
	}
}

// readLength parses a length header line. -1 (null) is allowed; anything
// below that or above max is rejected.
func (r *Reader) readLength(max int, kind string) (int, error) {
	line, err := r.readLine(64, true)
	if err != nil {
		return 0, unexpectedEOF(err)
	}
	n, err := strconv.Atoi(string(line))
	if err != nil || n < -1 || n > max {
		return 0, protoErr("invalid %s length", kind)
	}
	return n, nil
}

func (r *Reader) readBulk(n int) ([]byte, error) {
	var data []byte
	if n <= 64*1024 {
		data = make([]byte, n)
		if _, err := io.ReadFull(r.br, data); err != nil {
			return nil, unexpectedEOF(err)
		}
	} else {
		// Grow with the data actually received rather than trusting the
		// declared length for a single huge allocation.
		var buf bytes.Buffer
		if _, err := io.CopyN(&buf, r.br, int64(n)); err != nil {
			return nil, unexpectedEOF(err)
		}
		data = buf.Bytes()
	}
	cr, err := r.br.ReadByte()
	if err != nil {
		return nil, unexpectedEOF(err)
	}
	lf, err := r.br.ReadByte()
	if err != nil {
		return nil, unexpectedEOF(err)
	}
	if cr != '\r' || lf != '\n' {
		return nil, protoErr("expected CRLF after bulk data")
	}
	return data, nil
}

// readLine returns the next line without its terminator. The returned slice
// may alias the internal buffer and is only valid until the next read.
func (r *Reader) readLine(max int, requireCR bool) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.br.ReadSlice('\n')
		if line == nil && err == nil {
			line = chunk
		} else {
			line = append(line, chunk...)
		}
		if len(line) > max+2 {
			return nil, protoErr("line too long")
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	line = line[:len(line)-1]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		return line[:len(line)-1], nil
	}
	if requireCR {
		return nil, protoErr("expected CRLF line terminator")
	}
	return line, nil
}

func unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
