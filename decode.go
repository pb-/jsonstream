package jsonstream

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// Token represents any valid json token
type Token interface{}

// Delim is a delimiter token like {, [, ], or }
type Delim rune

// ErrNotString occurs when you request a string reader of a non-string token
var ErrNotString = errors.New("token is not a string value")

// NewDecoder creates a new decoder from an io.Reader
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		source: r,
		buffer: make([]byte, bufferSize)[:0],
		line:   1,
		column: 1,
	}
}

// Token returns the next token in the JSON stream
func (d *Decoder) Token() (Token, error) {
	input, err := d.skipWhitespace()
	if err != nil {
		return nil, err
	}

	token, err := d.readToken(input)
	if err != nil {
		return nil, err
	}

	return token, nil
}

// StringReader returns an io.Reader if the next token is a string or an ErrNotString error
func (d *Decoder) StringReader() (io.Reader, error) {
	input, err := d.skipWhitespace()
	if err != nil {
		return nil, err
	}

	if input != '"' {
		d.undo(input)
		return nil, ErrNotString
	}

	return d.newStringReader(), nil
}

// Decoder holds the decoder's internal state
type Decoder struct {
	source        io.Reader
	eof           bool
	line          int
	column        int
	buffer        []byte
	offset        int
	preparedByte  byte
	preparedValid bool
}

func (d *Decoder) next() (byte, error) {
	if d.preparedValid {
		d.preparedValid = false
		return d.preparedByte, nil
	}

	if d.offset < len(d.buffer) {
		b := d.buffer[d.offset]
		d.offset++
		return b, nil
	}

	n, err := d.source.Read(d.buffer[:cap(d.buffer)])
	if err != nil && err != io.EOF {
		return 0, err
	} else if n == 0 {
		return 0, io.EOF
	}

	d.buffer = d.buffer[:n]
	d.offset = 0

	return d.next()
}

func (d *Decoder) undo(input byte) {
	d.preparedByte = input
	d.preparedValid = true
}

func (d *Decoder) readToken(input byte) (Token, error) {
	if input == '{' || input == '}' || input == '[' || input == ']' {
		return Delim(input), nil
	} else if input == '"' {
		return d.readString()
	} else if input == 't' {
		return d.expect("rue", true)
	} else if input == 'f' {
		return d.expect("alse", false)
	} else if input == 'n' {
		return d.expect("ull", nil)
	} else if input == '-' || input == '.' || input >= '0' && input <= '9' {
		d.undo(input)
		return d.readNumber()
	}

	return nil, d.err(fmt.Errorf("bad input byte 0x%02x", input))
}

func (d *Decoder) expect(expected string, token Token) (Token, error) {
	for _, c := range []byte(expected) {
		input, err := d.next()
		if err != nil && err != io.EOF {
			return nil, err
		} else if err == io.EOF {
			return nil, d.err(errors.New("unexpected EOF while reading literal"))
		}

		if input != c {
			return nil, d.err(fmt.Errorf("unexpected input 0x%02x while reading literal", input))
		}
	}

	return token, nil
}

func (d *Decoder) readString() (Token, error) {
	all, err := ioutil.ReadAll(d.newStringReader())
	if err != nil {
		return nil, fmt.Errorf("unable to read string: %s", err)
	}

	return string(all), nil
}

func (d *Decoder) readNumber() (Token, error) {
	buffer := make([]byte, 64)
	offset := 0
	isFloat := false

	for {
		input, err := d.next()
		if err != nil && err != io.EOF {
			return nil, err
		} else if err == io.EOF {
			break
		}

		if input == '.' || input == 'e' || input == 'E' {
			isFloat = true
		} else if (input < '0' || input > '9') && input != '-' {
			d.undo(input)
			break
		}

		if offset == len(buffer) {
			return nil, d.err(errors.New("number is too long"))
		}

		buffer[offset] = input
		offset++
	}

	if isFloat {
		f, err := strconv.ParseFloat(string(buffer[:offset]), 64)
		if err != nil {
			return nil, d.err(fmt.Errorf("failed to scan float: %s", err))
		}
		return f, nil
	}

	i, err := strconv.ParseInt(string(buffer[:offset]), 10, 64)
	if err != nil {
		return nil, d.err(fmt.Errorf("failed to scan int: %s", err))
	}

	return i, nil
}

// skipWhitespace skips whitespace, ',', and ':' characters
func (d *Decoder) skipWhitespace() (byte, error) {
	for {
		input, err := d.next()
		if err != nil {
			return 0, err
		}

		if input == '\n' {
			d.line++
			d.column = 1
		} else {
			d.column++
		}

		if input != ' ' && input != '\r' && input != '\n' &&
			input != '\t' && input != ',' && input != ':' {
			return input, nil
		}
	}
}

func (d *Decoder) err(e error) error {
	return fmt.Errorf("scan error on line %d at column %d: %s", d.line, d.column, e)
}

type stringReader struct {
	decoder      *Decoder
	eof          bool
	step         func(*stringReader, byte) (byte, bool, error)
	memory       [4]byte
	memoryOffset int
	outputBuffer []byte
	surrogate    rune
}

func (r *stringReader) Read(p []byte) (n int, err error) {
	if r.eof {
		return 0, io.EOF
	}

	for n < len(p) {
		// drain output buffer first if any
		if len(r.outputBuffer) > 0 {
			p[n] = r.outputBuffer[0]
			n++
			r.outputBuffer = r.outputBuffer[1:]
			continue
		}

		input, err := r.decoder.next()
		if err != nil && err != io.EOF {
			return 0, err
		} else if err == io.EOF {
			return 0, r.decoder.err(errors.New("unexpected EOF while reading string"))
		}

		output, ok, err := r.step(r, input)
		if err != nil {
			return n, err
		}

		if ok {
			p[n] = output
			n++
		}
	}

	return n, nil
}

func defaultStep(r *stringReader, input byte) (output byte, ok bool, err error) {
	if input == '"' {
		r.eof = true
		err = io.EOF
	} else if input == '\\' {
		r.step = escape
	} else {
		output = input
		ok = true
	}

	return
}

func escape(r *stringReader, input byte) (output byte, ok bool, err error) {
	if input == 'u' {
		r.step = unicode
		r.memoryOffset = 0
		return
	}

	r.step = defaultStep
	switch input {
	case '"':
		return '"', true, nil
	case '\\':
		return '\\', true, nil
	case '/':
		return '/', true, nil
	case 'b':
		return '\b', true, nil
	case 'f':
		return '\f', true, nil
	case 'n':
		return '\n', true, nil
	case 'r':
		return '\r', true, nil
	case 't':
		return '\t', true, nil
	default:
		return 0, false, r.decoder.err(fmt.Errorf(
			"bad escape character 0x%02x while reading string", input))
	}
}

func unicode(r *stringReader, input byte) (output byte, ok bool, err error) {
	if (input < '0' || '9' < input) && (input < 'a' || 'f' < input) && (input < 'A' || 'F' < input) {
		return 0, false, r.decoder.err(fmt.Errorf(
			"bad unicode escape character 0x%02x while reading string", input))
	}
	r.memory[r.memoryOffset] = input
	r.memoryOffset++

	if r.memoryOffset < len(r.memory) {
		return
	}

	cp := getu4(r.memory)
	if r.surrogate == 0 {
		if utf16.IsSurrogate(cp) {
			r.surrogate = cp
			r.step = surrogateEscape
		} else {
			s := utf8.EncodeRune(r.memory[:], cp)
			r.outputBuffer = r.memory[:s]
			r.step = defaultStep
		}
	} else {
		if !utf16.IsSurrogate(cp) {
			return 0, false, r.decoder.err(errors.New("incomplete surrogate pair"))
		}
		s := utf8.EncodeRune(r.memory[:], utf16.DecodeRune(r.surrogate, cp))
		r.outputBuffer = r.memory[:s]
		r.surrogate = 0
		r.step = defaultStep
	}

	return
}

func surrogateEscape(r *stringReader, input byte) (output byte, ok bool, err error) {
	if input != '\\' {
		return 0, false, r.decoder.err(fmt.Errorf(
			"expected '\\' for second surrogate, got bad byte 0x%02x", input))
	}
	r.step = surrogateEscapeU
	return
}

func surrogateEscapeU(r *stringReader, input byte) (output byte, ok bool, err error) {
	if input != 'u' {
		return 0, false, r.decoder.err(fmt.Errorf(
			"expected 'u' for second surrogate, got bad byte 0x%02x", input))
	}
	r.step = unicode
	r.memoryOffset = 0
	return
}

func (d *Decoder) newStringReader() *stringReader {
	return &stringReader{decoder: d, step: defaultStep}
}

func getu4(s [4]byte) rune {
	r, _ := strconv.ParseUint(string(s[:]), 16, 64)
	return rune(r)
}

const bufferSize = 1024
