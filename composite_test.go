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

// --- helpers --------------------------------------------------------------

// bb wraps a raw []byte as a zero-copy ByteBuf component.
func bb(s string) ByteBuf {
	return NewSharedByteBuf([]byte(s))
}

func bbBytes(bs []byte) ByteBuf {
	return NewSharedByteBuf(bs)
}

func asDefault(c CompositeByteBuf) *defaultCompositeByteBuf {
	return c.(*defaultCompositeByteBuf)
}

// --- interface contract ---------------------------------------------------

// The composite must satisfy ByteBuf at compile time (compile-time
// assertion lives in composite.go). Guard against accidental removal.
func TestComposite_SatisfiesByteBuf(t *testing.T) {
	var _ ByteBuf = NewCompositeByteBuf()
}

// --- AddComponent / AddComponents ----------------------------------------

func TestComposite_AddComponent_AccumulatesLength(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"), bb("de"), bb("fghi"))
	assert.Equal(t, 9, c.ReadableBytes())
	d := asDefault(c)
	assert.Equal(t, 3, len(d.components))
	assert.Equal(t, []byte("abc"), d.components[0].data)
	assert.Equal(t, []byte("fghi"), d.components[2].data)
}

func TestComposite_AddComponents_Variadic(t *testing.T) {
	c := NewCompositeByteBuf()
	c.AddComponents(bb("hi"), bb(" "), bb("world"))
	assert.Equal(t, 8, c.ReadableBytes())
	assert.Equal(t, []byte("hi world"), c.Bytes())
}

func TestComposite_AddComponent_EmptyIgnored(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"))
	c.AddComponent(NewSharedByteBuf(nil))
	c.AddComponent(NewSharedByteBuf([]byte{}))
	assert.Equal(t, 1, len(asDefault(c).components))
	assert.Equal(t, 3, c.ReadableBytes())
}

func TestComposite_AddComponent_NilPanics(t *testing.T) {
	c := NewCompositeByteBuf()
	assert.Panics(t, func() { c.AddComponent(nil) })
}

// Nested composite: AddComponent of a composite flattens so readable
// region is preserved without nesting.
func TestComposite_AddComponent_FlattensNestedComposite(t *testing.T) {
	inner := NewCompositeByteBuf(bb("AA"), bb("BB"))
	inner.Skip(1) // readable = "ABB"
	outer := NewCompositeByteBuf(bb("X"))
	outer.AddComponent(inner)
	// Check component count BEFORE Bytes to avoid lazy-consolidate side effect.
	d := asDefault(outer)
	// Original "X" + the two flattened sub components covering "A" and "BB".
	assert.Equal(t, 3, len(d.components))
	assert.Equal(t, []byte("XABB"), outer.BytesCopy())
}

// --- Zero-copy alias contract --------------------------------------------

func TestComposite_Bytes_SingleComponentZeroCopy(t *testing.T) {
	src := []byte("zero-copy")
	c := NewCompositeByteBuf(bbBytes(src))
	view := c.Bytes()
	assert.Equal(t, unsafe.SliceData(src), unsafe.SliceData(view))
}

func TestComposite_Bytes_MultiComponentLazyConsolidates(t *testing.T) {
	c := NewCompositeByteBuf(bb("hello "), bb("world"))
	before := len(asDefault(c).components)
	_ = c.Bytes()
	after := len(asDefault(c).components)
	assert.Equal(t, 2, before)
	assert.Equal(t, 1, after, "multi-component Bytes should consolidate to one component")
	// Second Bytes returns the same merged slice (mutable view contract).
	view := c.Bytes()
	view[0] = 'H'
	assert.Equal(t, []byte("Hello world"), c.Bytes())
}

func TestComposite_LiveReference(t *testing.T) {
	src := []byte("abcde")
	c := NewCompositeByteBuf(bbBytes(src))
	src[0] = 'A'
	assert.Equal(t, "Abcde", string(c.Bytes()))
}

// --- Read path -----------------------------------------------------------

func TestComposite_ReadByte_CrossesBoundaries(t *testing.T) {
	c := NewCompositeByteBuf(bb("AB"), bb("CD"))
	for _, want := range []byte{'A', 'B', 'C', 'D'} {
		got, err := c.ReadByte()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := c.ReadByte()
	assert.Equal(t, ErrInsufficientSize, err)
}

func TestComposite_Read_CrossBoundary(t *testing.T) {
	c := NewCompositeByteBuf(bb("ABC"), bb("DEFG"))
	p := make([]byte, 5)
	n, err := c.Read(p)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []byte("ABCDE"), p)
	assert.Equal(t, 2, c.ReadableBytes())
}

func TestComposite_Read_EOF(t *testing.T) {
	c := NewCompositeByteBuf()
	_, err := c.Read(make([]byte, 4))
	assert.Equal(t, io.EOF, err)
}

func TestComposite_ReadBytes_WithinComponent_ZeroCopy(t *testing.T) {
	src := []byte("abcdefghij")
	c := NewCompositeByteBuf(bbBytes(src))
	got := c.ReadBytes(5)
	assert.Equal(t, unsafe.SliceData(src), unsafe.SliceData(got))
}

func TestComposite_ReadBytes_AcrossComponents_Copies(t *testing.T) {
	c := NewCompositeByteBuf(bb("ABC"), bb("DEFG"))
	assert.Equal(t, []byte("ABCDE"), c.ReadBytes(5))
	assert.Equal(t, []byte("FG"), c.ReadBytes(2))
}

func TestComposite_ReadByteBuf(t *testing.T) {
	c := NewCompositeByteBuf(bb("AB"), bb("CD"))
	buf := c.ReadByteBuf(3)
	assert.Equal(t, []byte("ABC"), buf.Bytes())
	assert.Equal(t, 1, c.ReadableBytes())
}

func TestComposite_Skip(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"), bb("def"))
	c.Skip(4)
	assert.Equal(t, 2, c.ReadableBytes())
	assert.Equal(t, []byte("ef"), c.Bytes())
	assert.Panics(t, func() { c.Skip(10) })
	assert.Panics(t, func() { c.Skip(-1) })
}

func TestComposite_ReadInt_CrossBoundary(t *testing.T) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], 0xDEADBEEF)
	c := NewCompositeByteBuf(bbBytes(buf[0:1]), bbBytes(buf[1:3]), bbBytes(buf[3:4]))
	assert.Equal(t, uint32(0xDEADBEEF), c.ReadUInt32())
	assert.Equal(t, 0, c.ReadableBytes())
}

func TestComposite_ReadInt_LE_CrossBoundary(t *testing.T) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], 0x0123456789ABCDEF)
	c := NewCompositeByteBuf(bbBytes(buf[0:3]), bbBytes(buf[3:5]), bbBytes(buf[5:8]))
	assert.Equal(t, uint64(0x0123456789ABCDEF), c.ReadUInt64LE())
}

func TestComposite_ReadFloat_RoundTrip(t *testing.T) {
	var buf4 [4]byte
	binary.BigEndian.PutUint32(buf4[:], math.Float32bits(math.Pi))
	c := NewCompositeByteBuf(bbBytes(buf4[0:1]), bbBytes(buf4[1:4]))
	assert.InDelta(t, float32(math.Pi), c.ReadFloat32(), 0)

	var buf8 [8]byte
	binary.LittleEndian.PutUint64(buf8[:], math.Float64bits(math.E))
	c2 := NewCompositeByteBuf(bbBytes(buf8[0:3]), bbBytes(buf8[3:8]))
	assert.InDelta(t, math.E, c2.ReadFloat64LE(), 0)
}

// --- Write path via tail --------------------------------------------------

func TestComposite_Write_Appends(t *testing.T) {
	c := NewCompositeByteBuf(bb("head|"))
	_, err := c.Write([]byte("body"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("head|body"), c.Bytes())
}

func TestComposite_WriteString(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteString("hello")
	assert.Equal(t, []byte("hello"), c.Bytes())
}

func TestComposite_WriteInt_RoundTrip(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteUInt16(0x1234)
	c.WriteUInt32(0xDEADBEEF)
	c.WriteUInt64(0x1122334455667788)
	assert.Equal(t, uint16(0x1234), c.ReadUInt16())
	assert.Equal(t, uint32(0xDEADBEEF), c.ReadUInt32())
	assert.Equal(t, uint64(0x1122334455667788), c.ReadUInt64())
}

func TestComposite_WriteInt_LE_RoundTrip(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteUInt16LE(0x1234)
	c.WriteUInt32LE(0xDEADBEEF)
	c.WriteUInt64LE(0x1122334455667788)
	assert.Equal(t, uint16(0x1234), c.ReadUInt16LE())
	assert.Equal(t, uint32(0xDEADBEEF), c.ReadUInt32LE())
	assert.Equal(t, uint64(0x1122334455667788), c.ReadUInt64LE())
}

func TestComposite_WriteFloat_RoundTrip(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteFloat32(math.Pi)
	c.WriteFloat64LE(math.E)
	assert.InDelta(t, float32(math.Pi), c.ReadFloat32(), 0)
	assert.InDelta(t, math.E, c.ReadFloat64LE(), 0)
}

// Interleaving Write and AddComponent seals the current tail and starts a
// fresh one on the next write, preserving order.
func TestComposite_Write_InterleaveWithAddComponent(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteString("A")
	c.AddComponent(bb("B"))
	c.WriteString("C")
	c.AddComponent(bb("D"))
	assert.Equal(t, []byte("ABCD"), c.Bytes())
}

func TestComposite_AppendByte(t *testing.T) {
	c := NewCompositeByteBuf()
	c.AppendByte('a').AppendByte('b').AppendByte('c')
	assert.Equal(t, []byte("abc"), c.Bytes())
}

func TestComposite_WriteByteBuf(t *testing.T) {
	c := NewCompositeByteBuf()
	src := EmptyByteBuf()
	src.WriteString("payload")
	c.WriteByteBuf(src)
	assert.Equal(t, []byte("payload"), c.Bytes())
}

func TestComposite_WriteReader(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteReader(bytes.NewReader([]byte("abcxyz")))
	assert.Equal(t, []byte("abcxyz"), c.Bytes())
}

// --- WriteAt --------------------------------------------------------------

// WriteAt inside an existing component aliases the source backing array —
// the source slice sees the mutation too.
func TestComposite_WriteAt_AliasExistingComponent(t *testing.T) {
	src := []byte("ABCDEFGH")
	c := NewCompositeByteBuf(bbBytes(src))
	n, err := c.WriteAt([]byte("xyz"), 2)
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "ABxyzFGH", string(src))
	assert.Equal(t, []byte("ABxyzFGH"), c.Bytes())
}

// WriteAt crossing component boundaries writes through both.
func TestComposite_WriteAt_AcrossComponents(t *testing.T) {
	a := []byte("AAAA")
	b := []byte("BBBB")
	c := NewCompositeByteBuf(bbBytes(a), bbBytes(b))
	_, err := c.WriteAt([]byte("xxxxxx"), 1)
	assert.NoError(t, err)
	assert.Equal(t, []byte("Axxx"), a)
	assert.Equal(t, []byte("xxxB"), b)
	assert.Equal(t, []byte("AxxxxxxB"), c.Bytes())
}

// WriteAt past writerIdx extends the tail; the gap is zero-filled.
func TestComposite_WriteAt_ExtendsPastEnd(t *testing.T) {
	c := NewCompositeByteBuf(bb("AB"))
	_, err := c.WriteAt([]byte("ZZ"), 4)
	assert.NoError(t, err)
	assert.Equal(t, 6, c.ReadableBytes())
	got := c.BytesCopy()
	assert.Equal(t, byte('A'), got[0])
	assert.Equal(t, byte('B'), got[1])
	assert.Equal(t, byte(0), got[2])
	assert.Equal(t, byte(0), got[3])
	assert.Equal(t, byte('Z'), got[4])
	assert.Equal(t, byte('Z'), got[5])
}

// --- Compact --------------------------------------------------------------

func TestComposite_Compact_DropsConsumedComponents(t *testing.T) {
	c := NewCompositeByteBuf(bb("ABC"), bb("DEF"), bb("GHI"))
	c.Skip(4) // inside second component
	c.Compact()
	assert.Equal(t, 0, c.ReaderIndex())
	assert.Equal(t, 5, c.ReadableBytes())
	d := asDefault(c)
	// First component dropped entirely; second trimmed to "EF"; third untouched.
	assert.Equal(t, 2, len(d.components))
	assert.Equal(t, []byte("EF"), d.components[0].data)
	assert.Equal(t, []byte("GHI"), d.components[1].data)
	assert.Equal(t, []byte("EFGHI"), c.Bytes())
}

func TestComposite_Compact_NoOpAtZero(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"))
	c.Compact()
	assert.Equal(t, []byte("abc"), c.Bytes())
}

func TestComposite_Compact_FullyDrained(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"))
	c.Skip(3)
	c.Compact()
	assert.Equal(t, 0, c.ReadableBytes())
	assert.Equal(t, 0, len(asDefault(c).components))
}

// --- Clone ----------------------------------------------------------------

func TestComposite_Clone_Independent(t *testing.T) {
	src := []byte("abcd")
	c := NewCompositeByteBuf(bbBytes(src), bb("efgh"))
	clone := c.Clone()
	// Clone is a DefaultByteBuf and independent of source mutations.
	src[0] = 'Z'
	assert.Equal(t, []byte("abcdefgh"), clone.Bytes())
	// Composite still sees the source mutation because of zero-copy alias.
	assert.Equal(t, []byte("Zbcdefgh"), c.Bytes())
}

func TestComposite_Clone_Empty(t *testing.T) {
	c := NewCompositeByteBuf()
	clone := c.Clone()
	assert.Equal(t, 0, clone.ReadableBytes())
}

// --- Slicer ---------------------------------------------------------------

func TestComposite_Slice_SingleComponent(t *testing.T) {
	c := NewCompositeByteBuf(bb("ABCDEFG"))
	v := c.Slice(2, 3)
	assert.Equal(t, []byte("CDE"), v.Bytes())
}

func TestComposite_Slice_AcrossComponents(t *testing.T) {
	c := NewCompositeByteBuf(bb("AB"), bb("CD"), bb("EF"))
	v := c.Slice(1, 4)
	assert.Equal(t, []byte("BCDE"), v.Bytes())
	// Parent unchanged.
	assert.Equal(t, 6, c.ReadableBytes())
}

func TestComposite_Slice_Alias(t *testing.T) {
	src := []byte("ABCDEF")
	c := NewCompositeByteBuf(bbBytes(src))
	v := c.Slice(1, 3)
	// Mutating the slice propagates to the source.
	v.Bytes()[0] = 'x'
	assert.Equal(t, []byte("AxCDEF"), src)
}

func TestComposite_Duplicate_IndependentIndices(t *testing.T) {
	c := NewCompositeByteBuf(bb("abcdef"))
	dup := c.Duplicate()
	c.Skip(3)
	assert.Equal(t, 3, c.ReadableBytes())
	assert.Equal(t, 6, dup.ReadableBytes())
}

func TestComposite_ReadSlice_Advances(t *testing.T) {
	c := NewCompositeByteBuf(bb("ABC"), bb("DEF"))
	v := c.ReadSlice(4)
	assert.Equal(t, []byte("ABCD"), v.Bytes())
	assert.Equal(t, 2, c.ReadableBytes())
}

// --- Mark / Reset ---------------------------------------------------------

func TestComposite_MarkResetReaderIndex(t *testing.T) {
	c := NewCompositeByteBuf(bb("abcdef"))
	c.Skip(2)
	c.MarkReaderIndex()
	c.Skip(3)
	assert.Equal(t, 5, c.ReaderIndex())
	c.ResetReaderIndex()
	assert.Equal(t, 2, c.ReaderIndex())
}

func TestComposite_MarkResetWriterIndex(t *testing.T) {
	c := NewCompositeByteBuf()
	c.WriteString("abc")
	c.MarkWriterIndex()
	c.WriteString("def")
	assert.Equal(t, 6, c.WriterIndex())
	c.ResetWriterIndex()
	assert.Equal(t, 3, c.WriterIndex())
}

// --- Cap / EnsureCapacity / Grow -----------------------------------------

func TestComposite_EnsureCapacity_GrowsTail(t *testing.T) {
	c := NewCompositeByteBuf()
	c.EnsureCapacity(1024)
	assert.GreaterOrEqual(t, c.Cap(), 1024)
}

func TestComposite_Grow_AddsCapacity(t *testing.T) {
	c := NewCompositeByteBuf()
	before := c.Cap()
	c.Grow(512)
	assert.GreaterOrEqual(t, c.Cap()-before, 512)
}

// --- Close / Reset --------------------------------------------------------

func TestComposite_Close_ClearsAll(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"))
	err := c.Close()
	assert.NoError(t, err)
	assert.Equal(t, 0, c.ReadableBytes())
	assert.Equal(t, 0, c.Cap())
}

func TestComposite_Reset_ClearsIndices(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"))
	c.Skip(2)
	c.Reset()
	assert.Equal(t, 0, c.ReaderIndex())
	assert.Equal(t, 0, c.WriterIndex())
}

// --- WriteTo --------------------------------------------------------------

func TestComposite_WriteTo_DrainsAndAdvances(t *testing.T) {
	c := NewCompositeByteBuf(bb("abc"), bb("de"), bb("fg"))
	var w bytes.Buffer
	n, err := c.WriteTo(&w)
	assert.NoError(t, err)
	assert.Equal(t, int64(7), n)
	assert.Equal(t, "abcdefg", w.String())
	assert.Equal(t, 0, c.ReadableBytes())
}

type failingWriter struct{ max int }

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
	c := NewCompositeByteBuf(bb("abcdef"))
	w := &failingWriter{max: 3}
	n, err := c.WriteTo(w)
	assert.Equal(t, int64(3), n)
	assert.Error(t, err)
	assert.Equal(t, 3, c.ReadableBytes())
}

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

	c := NewCompositeByteBuf(bb("HEAD|"), bb("MID|"), bb("TAIL"))
	n, err := c.WriteTo(client)
	assert.NoError(t, err)
	assert.Equal(t, int64(13), n)
	assert.NoError(t, client.(*net.TCPConn).CloseWrite())

	got, err := io.ReadAll(server)
	assert.NoError(t, err)
	assert.Equal(t, "HEAD|MID|TAIL", string(got))
}

// --- Refcount -------------------------------------------------------------

func TestComposite_Refcount(t *testing.T) {
	c := NewCompositeByteBuf(bb("x"))
	assert.Equal(t, int32(1), c.RefCnt())
	c.Retain()
	assert.Equal(t, int32(2), c.RefCnt())
	assert.False(t, c.Release())
	assert.True(t, c.Release())
	assert.Panics(t, func() { c.Release() })
}

// --- Pool does not accept composite --------------------------------------

func TestComposite_NotPooled(t *testing.T) {
	c := NewCompositeByteBuf(bb("x"))
	// ReleaseByteBuf is a type-checked no-op for composite; should not panic.
	ReleaseByteBuf(c)
}
