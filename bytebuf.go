package buf

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync/atomic"
)

// ByteBuf defines a byte buffer interface that is NOT concurrent-safe.
//
// Notes:
// - Unless specified otherwise, out-of-bound or invalid operations will panic (preserving the original semantics).
// - Bytes returns a mutable view; writing to the returned slice will mutate the internal buffer.
// - Use BytesCopy to obtain an immutable copy if you need isolation from internal mutations.
type ByteBuf interface {
	io.Writer
	io.Reader
	io.Closer
	io.WriterAt
	ReaderIndex() int
	WriterIndex() int
	MarkReaderIndex() ByteBuf
	ResetReaderIndex() ByteBuf
	MarkWriterIndex() ByteBuf
	ResetWriterIndex() ByteBuf
	Reset() ByteBuf
	Bytes() []byte
	BytesCopy() []byte
	ReadableBytes() int
	Cap() int
	Grow(v int) ByteBuf
	// Compact moves the readable region to the beginning of the buffer and adjusts indices (including marked indices).
	Compact() ByteBuf
	// EnsureCapacity guarantees that at least n bytes of writable space
	// are available. It compacts when that alone suffices; otherwise it
	// doubles the capacity to the smallest power-of-two that fits the
	// required size and compacts the readable region to index 0.
	EnsureCapacity(n int) ByteBuf
	Skip(v int) ByteBuf
	Clone() ByteBuf
	// AppendByte appends a single byte and returns the ByteBuf for chaining
	AppendByte(c byte) ByteBuf
	WriteBytes(bs []byte) ByteBuf
	WriteString(s string) ByteBuf
	WriteByteBuf(buf ByteBuf) ByteBuf
	WriteReader(reader io.Reader) ByteBuf
	WriteInt16(v int16) ByteBuf
	WriteInt32(v int32) ByteBuf
	WriteInt64(v int64) ByteBuf
	WriteUInt16(v uint16) ByteBuf
	WriteUInt32(v uint32) ByteBuf
	WriteUInt64(v uint64) ByteBuf
	WriteFloat32(v float32) ByteBuf
	WriteFloat64(v float64) ByteBuf
	WriteInt16LE(v int16) ByteBuf
	WriteInt32LE(v int32) ByteBuf
	WriteInt64LE(v int64) ByteBuf
	WriteUInt16LE(v uint16) ByteBuf
	WriteUInt32LE(v uint32) ByteBuf
	WriteUInt64LE(v uint64) ByteBuf
	WriteFloat32LE(v float32) ByteBuf
	WriteFloat64LE(v float64) ByteBuf
	// Standard Go io.ByteWriter interface
	WriteByte(c byte) error
	// MustReadByte reads a single byte and panics if insufficient data (existing behavior)
	MustReadByte() byte
	// Standard Go io.ByteReader interface
	ReadByte() (byte, error)
	ReadBytes(len int) []byte
	ReadByteBuf(len int) ByteBuf
	ReadWriter(writer io.Writer) ByteBuf
	ReadInt16() int16
	ReadInt32() int32
	ReadInt64() int64
	ReadUInt16() uint16
	ReadUInt32() uint32
	ReadUInt64() uint64
	ReadFloat32() float32
	ReadFloat64() float64
	ReadInt16LE() int16
	ReadInt32LE() int32
	ReadInt64LE() int64
	ReadUInt16LE() uint16
	ReadUInt32LE() uint32
	ReadUInt64LE() uint64
	ReadFloat32LE() float32
	ReadFloat64LE() float64
}

var ErrNilObject = errors.New("nil object")
var ErrInsufficientSize = errors.New("insufficient size")
var ErrRefCountUnderflow = errors.New("refcount underflow")

// Slicer is implemented by ByteBufs that expose zero-copy view APIs sharing
// the same backing storage as the parent.
type Slicer interface {
	Slice(from, length int) ByteBuf
	Duplicate() ByteBuf
	ReadSlice(n int) ByteBuf
}

// RefCounted is implemented by ByteBufs that participate in reference
// counted lifecycle management. Fresh buffers start at refcount 1.
type RefCounted interface {
	Retain() ByteBuf
	Release() bool
	RefCnt() int32
}

// newDefaultByteBuf constructs a DefaultByteBuf with refcount 1 and a
// poolIdx of -1 (unpooled).
func newDefaultByteBuf() *DefaultByteBuf {
	b := &DefaultByteBuf{poolIdx: -1}
	b.refcnt.Store(1)
	return b
}

func NewByteBuf(bs []byte) ByteBuf {
	buf := newDefaultByteBuf()
	buf.WriteBytes(bs)
	return buf
}

func NewByteBufString(str string) ByteBuf {
	buf := newDefaultByteBuf()
	buf.WriteString(str)
	return buf
}

func EmptyByteBuf() ByteBuf {
	return newDefaultByteBuf()
}

// NewSharedByteBuf wraps bs without copying. Writes that fit cap(bs) mutate
// the original backing array; writes that exceed it detach into a freshly
// allocated array and leave bs untouched.
func NewSharedByteBuf(bs []byte) ByteBuf {
	buf := newDefaultByteBuf()
	buf.buf = bs
	buf.writerIndex = len(bs)
	return buf
}

type DefaultByteBuf struct {
	buf                                                        []byte
	readerIndex, writerIndex, prevReaderIndex, prevWriterIndex int
	// poolIdx is the sync.Pool size class index this buffer belongs to,
	// or -1 for unpooled buffers (direct allocation or a view created by
	// Slice/Duplicate/ReadSlice). ReleaseByteBuf returns to the pool only
	// when poolIdx >= 0.
	poolIdx int32
	refcnt  atomic.Int32
}

func (b *DefaultByteBuf) Write(p []byte) (n int, err error) {
	pl := len(p)
	if pl == 0 {
		return 0, nil
	}

	b.prepare(pl)
	copy(b.buf[b.writerIndex:], p)
	b.writerIndex += pl
	return pl, nil
}

func (b *DefaultByteBuf) Read(p []byte) (n int, err error) {
	cpLen := b.ReadableBytes()
	if cpLen == 0 {
		return 0, io.EOF
	}

	if cpLen > len(p) {
		cpLen = len(p)
	}

	copy(p, b.buf[b.readerIndex:b.readerIndex+cpLen])
	b.readerIndex += cpLen
	return cpLen, nil
}

func (b *DefaultByteBuf) WriteAt(p []byte, offset int64) (n int, err error) {
	pl := len(p)
	if pl == 0 {
		return 0, nil
	}

	// Validate offset and prevent int overflow when converting to index
	maxInt := int(^uint(0) >> 1)
	if offset < 0 || offset > int64(maxInt-pl) {
		panic(ErrInsufficientSize)
	}
	off := int(offset)

	expLen := off + pl
	if expLen > b.Cap() {
		b.prepare(expLen - b.Cap())
	}
	if expLen > b.writerIndex {
		b.writerIndex = expLen
	}

	copy(b.buf[off:], p)
	return pl, nil
}

// Close drops the backing array and clears all indices so the buffer holds
// no storage. Cap() returns 0 afterwards.
func (b *DefaultByteBuf) Close() error {
	b.buf = nil
	b.readerIndex = 0
	b.writerIndex = 0
	b.prevReaderIndex = 0
	b.prevWriterIndex = 0
	return nil
}

func (b *DefaultByteBuf) ReaderIndex() int {
	return b.readerIndex
}

func (b *DefaultByteBuf) WriterIndex() int {
	return b.writerIndex
}

func (b *DefaultByteBuf) MarkReaderIndex() ByteBuf {
	b.prevReaderIndex = b.readerIndex
	return b
}

func (b *DefaultByteBuf) ResetReaderIndex() ByteBuf {
	b.readerIndex = b.prevReaderIndex
	b.prevReaderIndex = 0
	return b
}

func (b *DefaultByteBuf) MarkWriterIndex() ByteBuf {
	b.prevWriterIndex = b.writerIndex
	return b
}

func (b *DefaultByteBuf) ResetWriterIndex() ByteBuf {
	b.writerIndex = b.prevWriterIndex
	b.prevWriterIndex = 0
	return b
}

// Reset clears all indices (reader, writer, and marks) while keeping the
// backing array. Cap() is unchanged. Use Close to release the backing array.
func (b *DefaultByteBuf) Reset() ByteBuf {
	b.readerIndex = 0
	b.writerIndex = 0
	b.prevReaderIndex = 0
	b.prevWriterIndex = 0
	return b
}

func (b *DefaultByteBuf) Bytes() []byte {
	// Returns a mutable view of the readable region.
	// Mutating the returned slice will mutate the internal buffer directly.
	return b.buf[b.readerIndex:b.writerIndex]
}

func (b *DefaultByteBuf) BytesCopy() []byte {
	// Returns a copy of the readable region. Modifying the returned slice will not affect the internal buffer.
	if b.readerIndex == b.writerIndex {
		return []byte{}
	}
	cp := make([]byte, b.writerIndex-b.readerIndex)
	copy(cp, b.buf[b.readerIndex:b.writerIndex])
	return cp
}

// Compact moves the readable region to the beginning of the buffer and adjusts indices (including marked indices).
func (b *DefaultByteBuf) Compact() ByteBuf {
	if b.readerIndex == 0 {
		return b
	}
	readable := b.ReadableBytes()
	if readable > 0 {
		copy(b.buf[0:], b.buf[b.readerIndex:b.writerIndex])
	}
	shift := b.readerIndex
	b.readerIndex = 0
	b.writerIndex = readable
	if b.prevReaderIndex > 0 {
		if b.prevReaderIndex >= shift {
			b.prevReaderIndex -= shift
		} else {
			b.prevReaderIndex = 0
		}
	}
	if b.prevWriterIndex > 0 {
		if b.prevWriterIndex >= shift {
			b.prevWriterIndex -= shift
		} else {
			b.prevWriterIndex = 0
		}
	}
	return b
}

// EnsureCapacity guarantees that at least n bytes of writable space are
// available. It compacts when that alone suffices; otherwise it doubles
// the capacity to the smallest power-of-two that fits the required size
// and compacts the readable region to index 0.
func (b *DefaultByteBuf) EnsureCapacity(n int) ByteBuf {
	if n < 0 {
		panic(ErrInsufficientSize)
	}
	if n == 0 {
		return b
	}
	if b.writerIndex+n <= b.Cap() {
		return b
	}
	// First try to compact if total capacity can satisfy after compaction
	if b.ReadableBytes()+n <= b.Cap() {
		return b.Compact()
	}
	// Double the capacity until it holds the existing readable region
	// plus n writable bytes, then reallocate once via growTo which also
	// compacts the readable region to index 0.
	required := b.ReadableBytes() + n
	newCap := b.Cap()
	if newCap == 0 {
		newCap = 32
	}
	for newCap < required {
		newCap *= 2
	}
	b.growTo(newCap)
	return b
}

func (b *DefaultByteBuf) ReadableBytes() int {
	return b.writerIndex - b.readerIndex
}

func (b *DefaultByteBuf) Cap() int {
	return len(b.buf)
}

func (b *DefaultByteBuf) Grow(v int) ByteBuf {
	if v <= 0 {
		return b
	}

	// Calculate the minimum offset to preserve marked indices
	var offset int
	if b.prevReaderIndex == 0 {
		offset = b.readerIndex
	} else {
		offset = b.prevReaderIndex
		b.prevReaderIndex = 0
	}

	// Only copy the active data region (from offset to writerIndex)
	activeSize := b.writerIndex - offset
	tb := make([]byte, b.Cap()+v)

	if activeSize > 0 {
		copy(tb, b.buf[offset:b.writerIndex])
	}

	// Adjust indices
	b.readerIndex -= offset
	b.writerIndex -= offset
	if b.prevWriterIndex > 0 {
		b.prevWriterIndex -= offset
	}

	b.buf = tb
	return b
}

// Skip advances readerIndex by v bytes using only index arithmetic. Panics
// on a negative v or when v exceeds ReadableBytes.
func (b *DefaultByteBuf) Skip(v int) ByteBuf {
	if v < 0 {
		panic(ErrInsufficientSize)
	}
	if v == 0 {
		return b
	}
	if b.ReadableBytes() < v {
		panic(ErrInsufficientSize)
	}
	b.readerIndex += v
	return b
}

func (b *DefaultByteBuf) Clone() ByteBuf {
	// Optimize: directly copy readable data without creating intermediate slice
	readable := b.ReadableBytes()
	if readable == 0 {
		return EmptyByteBuf()
	}

	clone := newDefaultByteBuf()
	clone.buf = make([]byte, readable)
	copy(clone.buf, b.buf[b.readerIndex:b.writerIndex])
	clone.writerIndex = readable
	return clone
}

// AppendByte appends a single byte and returns the ByteBuf for chaining.
func (b *DefaultByteBuf) AppendByte(c byte) ByteBuf {
	b.prepare(1)
	b.buf[b.writerIndex] = c
	b.writerIndex++
	return b
}

// WriteByte implements io.ByteWriter interface with error return
func (b *DefaultByteBuf) WriteByte(c byte) error {
	b.prepare(1)
	b.buf[b.writerIndex] = c
	b.writerIndex++
	return nil // ByteBuf operations are designed to not fail for memory allocation
}

func (b *DefaultByteBuf) WriteBytes(bs []byte) ByteBuf {
	pl := len(bs)
	b.prepare(pl)
	copy(b.buf[b.writerIndex:], bs)
	b.writerIndex += pl
	return b
}

func (b *DefaultByteBuf) WriteByteBuf(buf ByteBuf) ByteBuf {
	if buf == nil {
		panic(ErrNilObject)
	}

	b.WriteBytes(buf.Bytes())
	return b
}

// writeReaderChunk is the minimum writable tail WriteReader keeps available
// before handing the underlying slice to reader.Read.
const writeReaderChunk = 4 * 1024

func (b *DefaultByteBuf) WriteReader(reader io.Reader) ByteBuf {
	if reader == nil {
		panic(ErrNilObject)
	}

	for {
		if b.Cap()-b.writerIndex < writeReaderChunk {
			b.prepare(writeReaderChunk)
		}
		n, err := reader.Read(b.buf[b.writerIndex:b.Cap()])
		if n > 0 {
			b.writerIndex += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		if n == 0 { // defensive break in case of weird readers
			break
		}
	}

	return b
}

func (b *DefaultByteBuf) WriteString(s string) ByteBuf {
	sl := len(s)
	if sl == 0 {
		return b
	}
	b.prepare(sl)
	copy(b.buf[b.writerIndex:], s)
	b.writerIndex += sl
	return b
}

func (b *DefaultByteBuf) WriteInt16(v int16) ByteBuf {
	b.WriteUInt16(uint16(v))
	return b
}

func (b *DefaultByteBuf) WriteInt32(v int32) ByteBuf {
	b.WriteUInt32(uint32(v))
	return b
}

func (b *DefaultByteBuf) WriteInt64(v int64) ByteBuf {
	b.WriteUInt64(uint64(v))
	return b
}

func (b *DefaultByteBuf) WriteUInt16(v uint16) ByteBuf {
	b.prepare(2)
	binary.BigEndian.PutUint16(b.buf[b.writerIndex:], v)
	b.writerIndex += 2
	return b
}

func (b *DefaultByteBuf) WriteUInt32(v uint32) ByteBuf {
	b.prepare(4)
	binary.BigEndian.PutUint32(b.buf[b.writerIndex:], v)
	b.writerIndex += 4
	return b
}

func (b *DefaultByteBuf) WriteUInt64(v uint64) ByteBuf {
	b.prepare(8)
	binary.BigEndian.PutUint64(b.buf[b.writerIndex:], v)
	b.writerIndex += 8
	return b
}

func (b *DefaultByteBuf) WriteFloat32(v float32) ByteBuf {
	b.prepare(4)
	binary.BigEndian.PutUint32(b.buf[b.writerIndex:], math.Float32bits(v))
	b.writerIndex += 4
	return b
}

func (b *DefaultByteBuf) WriteFloat64(v float64) ByteBuf {
	b.prepare(8)
	binary.BigEndian.PutUint64(b.buf[b.writerIndex:], math.Float64bits(v))
	b.writerIndex += 8
	return b
}

func (b *DefaultByteBuf) WriteInt16LE(v int16) ByteBuf {
	b.WriteUInt16LE(uint16(v))
	return b
}

func (b *DefaultByteBuf) WriteInt32LE(v int32) ByteBuf {
	b.WriteUInt32LE(uint32(v))
	return b
}

func (b *DefaultByteBuf) WriteInt64LE(v int64) ByteBuf {
	b.WriteUInt64LE(uint64(v))
	return b
}

func (b *DefaultByteBuf) WriteUInt16LE(v uint16) ByteBuf {
	b.prepare(2)
	binary.LittleEndian.PutUint16(b.buf[b.writerIndex:], v)
	b.writerIndex += 2
	return b
}

func (b *DefaultByteBuf) WriteUInt32LE(v uint32) ByteBuf {
	b.prepare(4)
	binary.LittleEndian.PutUint32(b.buf[b.writerIndex:], v)
	b.writerIndex += 4
	return b
}

func (b *DefaultByteBuf) WriteUInt64LE(v uint64) ByteBuf {
	b.prepare(8)
	binary.LittleEndian.PutUint64(b.buf[b.writerIndex:], v)
	b.writerIndex += 8
	return b
}

func (b *DefaultByteBuf) WriteFloat32LE(v float32) ByteBuf {
	b.prepare(4)
	binary.LittleEndian.PutUint32(b.buf[b.writerIndex:], math.Float32bits(v))
	b.writerIndex += 4
	return b
}

func (b *DefaultByteBuf) WriteFloat64LE(v float64) ByteBuf {
	b.prepare(8)
	binary.LittleEndian.PutUint64(b.buf[b.writerIndex:], math.Float64bits(v))
	b.writerIndex += 8
	return b
}

// MustReadByte reads a single byte and panics if insufficient data (original ReadByte behavior)
func (b *DefaultByteBuf) MustReadByte() byte {
	if b.readerIndex == b.writerIndex {
		panic(ErrInsufficientSize)
	}

	b.readerIndex++
	return b.buf[b.readerIndex-1]
}

// ReadByte implements io.ByteReader interface with error return
func (b *DefaultByteBuf) ReadByte() (byte, error) {
	if b.readerIndex == b.writerIndex {
		return 0, ErrInsufficientSize
	}

	b.readerIndex++
	return b.buf[b.readerIndex-1], nil
}

func (b *DefaultByteBuf) ReadBytes(len int) []byte {
	if len < 0 {
		panic(ErrInsufficientSize)
	}
	if len == 0 {
		return []byte{}
	}

	if b.ReadableBytes() < len {
		panic(ErrInsufficientSize)
	}

	b.readerIndex += len
	return b.buf[b.readerIndex-len : b.readerIndex]
}

func (b *DefaultByteBuf) ReadByteBuf(len int) ByteBuf {
	buf := newDefaultByteBuf()
	buf.WriteBytes(b.ReadBytes(len))
	return buf
}

func (b *DefaultByteBuf) ReadWriter(writer io.Writer) ByteBuf {
	bs := b.Bytes()
	n, err := writer.Write(bs)
	b.ReadBytes(n)
	if err != nil {
		panic(err)
	}

	return b
}

func (b *DefaultByteBuf) ReadInt16() int16 {
	return int16(b.ReadUInt16())
}

func (b *DefaultByteBuf) ReadInt32() int32 {
	return int32(b.ReadUInt32())
}

func (b *DefaultByteBuf) ReadInt64() int64 {
	return int64(b.ReadUInt64())
}

func (b *DefaultByteBuf) ReadUInt16() uint16 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 2 {
		panic(ErrInsufficientSize)
	}
	result := binary.BigEndian.Uint16(b.buf[b.readerIndex:])
	b.readerIndex += 2
	return result
}

func (b *DefaultByteBuf) ReadUInt32() uint32 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 4 {
		panic(ErrInsufficientSize)
	}
	result := binary.BigEndian.Uint32(b.buf[b.readerIndex:])
	b.readerIndex += 4
	return result
}

func (b *DefaultByteBuf) ReadUInt64() uint64 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 8 {
		panic(ErrInsufficientSize)
	}
	result := binary.BigEndian.Uint64(b.buf[b.readerIndex:])
	b.readerIndex += 8
	return result
}

func (b *DefaultByteBuf) ReadFloat32() float32 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 4 {
		panic(ErrInsufficientSize)
	}
	result := math.Float32frombits(binary.BigEndian.Uint32(b.buf[b.readerIndex:]))
	b.readerIndex += 4
	return result
}

func (b *DefaultByteBuf) ReadFloat64() float64 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 8 {
		panic(ErrInsufficientSize)
	}
	result := math.Float64frombits(binary.BigEndian.Uint64(b.buf[b.readerIndex:]))
	b.readerIndex += 8
	return result
}

func (b *DefaultByteBuf) ReadInt16LE() int16 {
	return int16(b.ReadUInt16LE())
}

func (b *DefaultByteBuf) ReadInt32LE() int32 {
	return int32(b.ReadUInt32LE())
}

func (b *DefaultByteBuf) ReadInt64LE() int64 {
	return int64(b.ReadUInt64LE())
}

func (b *DefaultByteBuf) ReadUInt16LE() uint16 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 2 {
		panic(ErrInsufficientSize)
	}
	result := binary.LittleEndian.Uint16(b.buf[b.readerIndex:])
	b.readerIndex += 2
	return result
}

func (b *DefaultByteBuf) ReadUInt32LE() uint32 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 4 {
		panic(ErrInsufficientSize)
	}
	result := binary.LittleEndian.Uint32(b.buf[b.readerIndex:])
	b.readerIndex += 4
	return result
}

func (b *DefaultByteBuf) ReadUInt64LE() uint64 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 8 {
		panic(ErrInsufficientSize)
	}
	result := binary.LittleEndian.Uint64(b.buf[b.readerIndex:])
	b.readerIndex += 8
	return result
}

func (b *DefaultByteBuf) ReadFloat32LE() float32 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 4 {
		panic(ErrInsufficientSize)
	}
	result := math.Float32frombits(binary.LittleEndian.Uint32(b.buf[b.readerIndex:]))
	b.readerIndex += 4
	return result
}

func (b *DefaultByteBuf) ReadFloat64LE() float64 {
	// Optimized: direct memory access without creating temporary slice
	if b.ReadableBytes() < 8 {
		panic(ErrInsufficientSize)
	}
	result := math.Float64frombits(binary.LittleEndian.Uint64(b.buf[b.readerIndex:]))
	b.readerIndex += 8
	return result
}

// prepare guarantees at least i more writable bytes at writerIndex, doubling
// the capacity in a single reallocation and compacting the readable region
// to the start when a new backing array is needed.
func (b *DefaultByteBuf) prepare(i int) {
	if i <= 0 {
		return
	}
	required := b.writerIndex + i
	if required <= b.Cap() {
		return
	}

	newCap := b.Cap()
	if newCap == 0 {
		newCap = 32
	}
	for newCap < required {
		newCap *= 2
	}

	b.growTo(newCap)
}

// growTo reallocates the backing array to newCap and compacts the active
// region (from the oldest preserved index) to the start. Marked indices are
// adjusted so they remain valid after the move.
func (b *DefaultByteBuf) growTo(newCap int) {
	var offset int
	if b.prevReaderIndex == 0 {
		offset = b.readerIndex
	} else {
		offset = b.prevReaderIndex
		b.prevReaderIndex = 0
	}

	activeSize := b.writerIndex - offset
	tb := make([]byte, newCap)
	if activeSize > 0 {
		copy(tb, b.buf[offset:b.writerIndex])
	}

	b.readerIndex -= offset
	b.writerIndex -= offset
	if b.prevWriterIndex > 0 {
		b.prevWriterIndex -= offset
	}
	b.buf = tb
}

// Slice returns a view that shares the backing array with b. The view
// starts at the given offset within b's readable region and covers length
// bytes. Mutations through the view within its capacity are visible to b.
// A write that needs to grow beyond the initial capacity allocates a fresh
// backing array inside the view and detaches it from b.
func (b *DefaultByteBuf) Slice(from, length int) ByteBuf {
	if from < 0 || length < 0 || from+length > b.ReadableBytes() {
		panic(ErrInsufficientSize)
	}
	start := b.readerIndex + from
	s := newDefaultByteBuf()
	s.buf = b.buf[start : start+length]
	s.writerIndex = length
	return s
}

// Duplicate returns a ByteBuf that shares b's backing array but keeps its
// own reader, writer, and mark indices. Writes that fit within the shared
// capacity are visible to b; writes that grow detach the duplicate.
func (b *DefaultByteBuf) Duplicate() ByteBuf {
	d := newDefaultByteBuf()
	d.buf = b.buf
	d.readerIndex = b.readerIndex
	d.writerIndex = b.writerIndex
	return d
}

// ReadSlice advances readerIndex by n and returns a zero-copy view over the
// consumed bytes. The returned view shares b's backing array.
func (b *DefaultByteBuf) ReadSlice(n int) ByteBuf {
	if n < 0 {
		panic(ErrInsufficientSize)
	}
	if b.ReadableBytes() < n {
		panic(ErrInsufficientSize)
	}
	start := b.readerIndex
	b.readerIndex += n
	s := newDefaultByteBuf()
	s.buf = b.buf[start : start+n]
	s.writerIndex = n
	return s
}

// Retain increments the reference count and returns the buffer for chaining.
func (b *DefaultByteBuf) Retain() ByteBuf {
	b.refcnt.Add(1)
	return b
}

// Release decrements the reference count. It returns true when the counter
// reaches zero, at which point the caller owns the final drop. Panics on
// underflow.
func (b *DefaultByteBuf) Release() bool {
	n := b.refcnt.Add(-1)
	if n < 0 {
		panic(ErrRefCountUnderflow)
	}
	return n == 0
}

// RefCnt returns the current reference count.
func (b *DefaultByteBuf) RefCnt() int32 {
	return b.refcnt.Load()
}
