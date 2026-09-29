package protocol

import (
	"strconv"
	"strings"
)

// AppendValue appends the RESP encoding of v to dst.
func AppendValue(dst []byte, v Value) []byte {
	switch v.Type {
	case TypeSimpleString, TypeError:
		dst = append(dst, byte(v.Type))
		dst = append(dst, sanitizeLine(v.Str)...)
		return append(dst, '\r', '\n')
	case TypeInteger:
		dst = append(dst, ':')
		dst = strconv.AppendInt(dst, v.Int, 10)
		return append(dst, '\r', '\n')
	case TypeBulkString:
		if v.Null {
			return append(dst, "$-1\r\n"...)
		}
		dst = append(dst, '$')
		dst = strconv.AppendInt(dst, int64(len(v.Bulk)), 10)
		dst = append(dst, '\r', '\n')
		dst = append(dst, v.Bulk...)
		return append(dst, '\r', '\n')
	case TypeArray:
		if v.Null {
			return append(dst, "*-1\r\n"...)
		}
		dst = AppendArrayHeader(dst, len(v.Array))
		for _, e := range v.Array {
			dst = AppendValue(dst, e)
		}
		return dst
	default:
		return append(dst, "-ERR internal: invalid reply type\r\n"...)
	}
}

// AppendArrayHeader appends "*<n>\r\n".
func AppendArrayHeader(dst []byte, n int) []byte {
	dst = append(dst, '*')
	dst = strconv.AppendInt(dst, int64(n), 10)
	return append(dst, '\r', '\n')
}

// AppendCommand encodes args as a RESP array of bulk strings (request form).
func AppendCommand(dst []byte, args ...[]byte) []byte {
	dst = AppendArrayHeader(dst, len(args))
	for _, a := range args {
		dst = AppendValue(dst, Bulk(a))
	}
	return dst
}

// Simple strings and errors are line-delimited, so embedded CR/LF (e.g. from
// an echoed client-supplied command name) would corrupt the stream.
func sanitizeLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
