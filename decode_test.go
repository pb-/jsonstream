package jsonstream

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmpty(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString(""))
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString("   \t   \n\n :,,, "))
	requireEOF(t, d)

	d = NewDecoder(newBadReader("  "))
	_, err := d.Token()
	require.Equal(t, errRandomIOError, err)
}

func TestBad(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString("\x00"))
	requireError(t, d)
}

func TestBasic(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString(`[false] } 11 2.2 "bar"nulltrue`))
	requireToken(t, d, Delim('['))
	requireToken(t, d, false)
	requireToken(t, d, Delim(']'))
	requireToken(t, d, Delim('}'))
	requireToken(t, d, int64(11))
	requireToken(t, d, 2.2)
	requireToken(t, d, "bar")
	requireToken(t, d, nil)
	requireToken(t, d, true)
	requireEOF(t, d)
}

func TestLiteral(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString("nul"))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString("nuls"))
	requireError(t, d)

	d = NewDecoder(newBadReader("fa"))
	_, err := d.Token()
	require.Equal(t, errRandomIOError, err)
}

func TestNumber(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString("-394"))
	requireToken(t, d, int64(-394))
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString("1e-3"))
	requireToken(t, d, 0.001)
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString("394["))
	requireToken(t, d, int64(394))
	requireToken(t, d, Delim('['))
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString("4.."))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString("9223372036854775808")) // too large for int64
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(strings.Repeat("9", 100))) // too large for buffer
	requireError(t, d)

	d = NewDecoder(newBadReader("0"))
	_, err := d.Token()
	require.Equal(t, errRandomIOError, err)
}

func TestNotString(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString(`false`))
	_, err := d.StringReader()
	require.Equal(t, ErrNotString, err)
	requireToken(t, d, false)
	requireEOF(t, d)
}

func TestStringReader(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString(`"foo bar"`))
	r, err := d.StringReader()
	require.NoError(t, err)
	data, err := ioutil.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "foo bar", string(data))

	// EOF idempotency
	data, err = ioutil.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "", string(data))

	d = NewDecoder(bytes.NewBufferString(""))
	r, err = d.StringReader()
	require.Equal(t, io.EOF, err)

	d = NewDecoder(newBadReader(`"foo`))
	r, err = d.StringReader()
	require.NoError(t, err)
	data, err = ioutil.ReadAll(r)
	require.Equal(t, errRandomIOError, err)
}

func TestString(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString(`"abc\x"`))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\"\\\/\b\f\n\r\t"`))
	requireToken(t, d, "\"\\/\b\f\n\r\t")
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\u0020\u2190"`))
	requireToken(t, d, " ←")
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\uD834\uDD1E"`))
	requireToken(t, d, "\U0001d11e")
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\uD834"`))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\uD834\u0020"`))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\uD834\t"`))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\uD83"`))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(`"\uD83`))
	requireError(t, d)

	d = NewDecoder(bytes.NewBufferString(`"🔥"`))
	requireToken(t, d, "🔥")
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString(`""`))
	requireToken(t, d, "")
	requireEOF(t, d)

	d = NewDecoder(bytes.NewBufferString(`"foo`))
	requireError(t, d)

	long := strings.Repeat(" ", bufferSize+10)
	d = NewDecoder(bytes.NewBufferString(`"` + long + `"`))
	requireToken(t, d, long)
	requireEOF(t, d)
}

func TestRefill(t *testing.T) {
	d := NewDecoder(bytes.NewBuffer(append(bytes.Repeat([]byte{' '}, bufferSize), []byte("null")...)))
	requireToken(t, d, nil)
	requireEOF(t, d)
}

var errRandomIOError = errors.New("random IO error")

type badReader struct {
	data []byte
}

// newBadReader creates a reader that will return an IO error (not EOF) after reading the specified string
func newBadReader(data string) *badReader {
	return &badReader{data: []byte(data)}
}

func (b *badReader) Read(p []byte) (int, error) {
	if len(b.data) > 0 {
		p[0] = b.data[0]
		b.data = b.data[1:]
		return 1, nil
	}

	return 0, errRandomIOError
}

func requireToken(t *testing.T, d *Decoder, token Token) {
	actual, err := d.Token()
	require.NoError(t, err)
	require.Equal(t, token, actual)
}

func requireEOF(t *testing.T, d *Decoder) {
	_, err := d.Token()
	require.Equal(t, io.EOF, err)
}

func requireError(t *testing.T, d *Decoder) {
	_, err := d.Token()
	require.Error(t, err)
	require.NotEqual(t, io.EOF, err)
}
