package buf

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"net"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// A composite formed from three slices reports the concatenated readable
// length and exposes each component verbatim.
func TestComposite_AddComponent_AccumulatesLength(t *testing.T) {
	c := NewCompositeByteBuf([]byte("abc"), []byte("de"), []byte("fghi"))
	assert.Equal(t, 9, c.ReadableBytes())
	assert.Equal(t, 3, c.NumComponents())
	assert.Equal(t, []byte("abc"), c.Component(0))
	assert.Equal(t, []byte("fghi"), c.Component(2))
}

// Adding a zero-length component is a no-op.
func TestComposite_AddComponent_EmptyIgnored(t *testing.T) {
	c := NewCompositeByteBuf([]byte("abc"))
	c.AddComponent(nil)
	c.AddComponent([]byte{})
	assert.Equal(t, 1, c.NumComponents())
	assert.Equal(t, 3, c.ReadableBytes())
}

// Bytes returns a zero-copy view when the readable region fits in one
// component.
func TestComposite_Bytes_SingleComponentZeroCopy(t *testing.T) {
	src := []byte("zero-copy")
	c := NewCompositeByteBuf(src)
	view := c.Bytes()
	assert.Equal(t, unsafe.SliceData(src), unsafe.SliceData(view))
}

// Bytes across multiple components produces a fresh copy.
func TestComposite_Bytes_MultiComponentCopies(t *testing.T) {
	c := NewCompositeByteBuf([]byte("hello "), []byte("world"))
	b := c.Bytes()
	assert.Equal(t, "hello world", string(b))
}

// Live reference contract: mutating the source slice in place is visible
// through the composite.
func TestComposite_LiveReference(t *testing.T) {
	src := []byte("abcde")
	c := NewCompositeByteBuf(src)
	src[0] = 'A'
	b := c.Bytes()
	assert.Equal(t, "Abcde", string(b))
}

// ReadByte walks component boundaries.
func TestComposite_ReadByte_CrossesBoundaries(t *testing.T) {
	c := NewCompositeByteBuf([]byte("AB"), []byte("CD"))
	for _, want := range []byte{'A', 'B', 'C', 'D'} {
		got, err := c.ReadByte()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := c.ReadByte()
	assert.Equal(t, ErrInsufficientSize, err)
}

// ReadBytes returns a zero-copy slice when the region stays within one
// component.
func TestComposite_ReadBytes_WithinComponent_ZeroCopy(t *testing.T) {
	src := []byte("abcdefghij")
	c := NewCompositeByteBuf(src)
	got := c.ReadBytes(5)
	assert.Equal(t, unsafe.SliceData(src), unsafe.SliceData(got))
	assert.Equal(t, []byte("abcde"), got)
}

// ReadBytes copies when the region spans two components.
func TestComposite_ReadBytes_AcrossComponents_Copies(t *testing.T) {
	c := NewCompositeByteBuf([]byte("ABC"), []byte("DEFG"))
	got := c.ReadBytes(5) // "ABCDE"
	assert.Equal(t, []byte("ABCDE"), got)
	// second read continues where the first left off
	assert.Equal(t, []byte("FG"), c.ReadBytes(2))
}

// Skip honors reader-only index arithmetic.
func TestComposite_Skip(t *testing.T) {
	c := NewCompositeByteBuf([]byte("abc"), []byte("def"))
	c.Skip(4)
	assert.Equal(t, 2, c.ReadableBytes())
	assert.Equal(t, []byte("ef"), c.Bytes())

	assert.Panics(t, func() { c.Skip(10) })
	assert.Panics(t, func() { c.Skip(-1) })
}

// Big-endian integer reads work across component boundaries.
func TestComposite_ReadInt_CrossBoundary(t *testing.T) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], 0xDEADBEEF)
	// split the 4 bytes across 3 components so the slow path is exercised
	c := NewCompositeByteBuf(buf[0:1], buf[1:3], buf[3:4])
	assert.Equal(t, uint32(0xDEADBEEF), c.ReadUInt32())
	assert.Equal(t, 0, c.ReadableBytes())
}

// Little-endian variants mirror big-endian semantics across boundaries.
func TestComposite_ReadInt_LE_CrossBoundary(t *testing.T) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], 0x0123456789ABCDEF)
	c := NewCompositeByteBuf(buf[0:3], buf[3:5], buf[5:8])
	assert.Equal(t, uint64(0x0123456789ABCDEF), c.ReadUInt64LE())
}

// Float conversions round-trip through the composite.
func TestComposite_ReadFloat_RoundTrip(t *testing.T) {
	var buf4 [4]byte
	binary.BigEndian.PutUint32(buf4[:], math.Float32bits(math.Pi))
	c := NewCompositeByteBuf(buf4[0:1], buf4[1:4])
	got := c.ReadFloat32()
	assert.InDelta(t, float32(math.Pi), got, 0)

	var buf8 [8]byte
	binary.LittleEndian.PutUint64(buf8[:], math.Float64bits(math.E))
	c2 := NewCompositeByteBuf(buf8[0:3], buf8[3:8])
	assert.InDelta(t, math.E, c2.ReadFloat64LE(), 0)
}

// DecomposeReadable yields the exact live slices that cover the readable
// region.
func TestComposite_DecomposeReadable_CoversReadableRegion(t *testing.T) {
	a := []byte("AA")
	b := []byte("BBB")
	d := []byte("DDDD")
	c := NewCompositeByteBuf(a, b, d)
	c.Skip(3) // reader at 'B' (second byte of second component)

	segs := c.DecomposeReadable()
	flat := bytes.Join(segs, nil)
	assert.Equal(t, []byte("BBDDDD"), flat)
}

// Empty composite yields nil segments.
func TestComposite_DecomposeReadable_EmptyReturnsNil(t *testing.T) {
	c := NewCompositeByteBuf()
	assert.Nil(t, c.DecomposeReadable())
}

// A composite backed by a ByteBuf receives a live snapshot of the readable
// region.
func TestComposite_AddByteBuf_Snapshot(t *testing.T) {
	src := EmptyByteBuf()
	src.WriteString("snapshot-data")
	c := NewCompositeByteBuf()
	c.AddByteBuf(src)
	assert.Equal(t, []byte("snapshot-data"), c.Bytes())
}

// WriteTo drains the composite into an io.Writer and advances readerIdx.
func TestComposite_WriteTo_DrainsAndAdvances(t *testing.T) {
	c := NewCompositeByteBuf([]byte("abc"), []byte("de"), []byte("fg"))
	var w bytes.Buffer
	n, err := c.WriteTo(&w)
	assert.NoError(t, err)
	assert.Equal(t, int64(7), n)
	assert.Equal(t, "abcdefg", w.String())
	assert.Equal(t, 0, c.ReadableBytes())
}

// WriteTo partial failure advances readerIdx only by bytes actually
// written.
type failingWriter struct {
	max int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.max <= 0 {
		return 0, io.ErrShortWrite
	}
	if f.max >= len(p) {
		f.max -= len(p)
		return len(p), nil
	}
	n := f.max
	f.max = 0
	return n, io.ErrShortWrite
}

func TestComposite_WriteTo_PartialWrite(t *testing.T) {
	c := NewCompositeByteBuf([]byte("abcdef"))
	w := &failingWriter{max: 3}
	n, err := c.WriteTo(w)
	assert.Equal(t, int64(3), n)
	assert.Error(t, err)
	assert.Equal(t, 3, c.ReadableBytes())
}

// Refcount semantics mirror DefaultByteBuf.
func TestComposite_Refcount(t *testing.T) {
	c := NewCompositeByteBuf([]byte("x"))
	assert.Equal(t, int32(1), c.RefCnt())
	c.Retain()
	assert.Equal(t, int32(2), c.RefCnt())
	assert.False(t, c.Release())
	assert.True(t, c.Release())
	assert.Panics(t, func() { c.Release() })
}

// net.Buffers integration: a composite.WriteTo into a TCP connection sends
// every component and the loopback peer receives the concatenated bytes.
func TestComposite_WriteTo_TCPConn_Integration(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer lis.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := net.Dial("tcp", lis.Addr().String())
	assert.NoError(t, err)
	defer client.Close()

	server := <-accepted
	defer server.Close()

	c := NewCompositeByteBuf(
		[]byte("HEAD|"),
		[]byte("MID|"),
		[]byte("TAIL"),
	)
	n, err := c.WriteTo(client)
	assert.NoError(t, err)
	assert.Equal(t, int64(13), n)
	assert.NoError(t, client.(*net.TCPConn).CloseWrite())

	got, err := io.ReadAll(server)
	assert.NoError(t, err)
	assert.Equal(t, "HEAD|MID|TAIL", string(got))
}
