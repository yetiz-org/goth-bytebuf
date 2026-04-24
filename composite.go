package buf

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"sync/atomic"
)

// ErrCompositeOutOfRange is raised when a composite read or slice request
// exceeds the available readable region.
var ErrCompositeOutOfRange = errors.New("composite byte buf: out of range")

// CompositeByteBuf is a read-centric zero-copy view over a sequence of
// component byte slices. Components are referenced, never copied, so that
// mutations to the source slices (within their original capacity) are
// visible through the composite. Writes to the composite append a new
// component and never touch earlier ones.
//
// CompositeByteBuf is NOT goroutine-safe.
type CompositeByteBuf interface {
	// AddComponent appends a component that shares bs with the caller.
	// Subsequent mutations of bs that stay within its original capacity
	// are visible through the composite.
	AddComponent(bs []byte) CompositeByteBuf

	// AddByteBuf snapshots the readable region of bb and appends it as a
	// component. The snapshot shares bb's backing array, so mutations of
	// that region through bb (or any other view) remain visible until bb
	// grows away from the backing array.
	AddByteBuf(bb ByteBuf) CompositeByteBuf

	// NumComponents returns the number of currently held components.
	NumComponents() int

	// Component returns the raw slice for the i-th component.
	Component(i int) []byte

	// DecomposeReadable returns the component slices covering the
	// readable region in order. The returned slices are live views — do
	// not mutate beyond their length unless you understand the aliasing.
	DecomposeReadable() [][]byte

	// ReadableBytes returns the number of bytes available for reading.
	ReadableBytes() int

	// ReaderIndex returns the current logical read position.
	ReaderIndex() int

	// WriterIndex returns the total length of all components.
	WriterIndex() int

	// Skip advances the reader by n bytes.
	Skip(n int) CompositeByteBuf

	// ReadByte consumes and returns a single byte.
	ReadByte() (byte, error)

	// ReadBytes returns the next n bytes. When the region lies in a
	// single component, the returned slice aliases that component. When
	// the region spans multiple components, a fresh copy is returned.
	ReadBytes(n int) []byte

	// Bytes returns the readable region. When all readable data lies in a
	// single component, the result aliases that component. Otherwise a
	// fresh copy is produced.
	Bytes() []byte

	// BytesCopy returns an independent copy of the readable region.
	BytesCopy() []byte

	ReadUInt16() uint16
	ReadUInt32() uint32
	ReadUInt64() uint64
	ReadInt16() int16
	ReadInt32() int32
	ReadInt64() int64
	ReadFloat32() float32
	ReadFloat64() float64
	ReadUInt16LE() uint16
	ReadUInt32LE() uint32
	ReadUInt64LE() uint64
	ReadInt16LE() int16
	ReadInt32LE() int32
	ReadInt64LE() int64
	ReadFloat32LE() float32
	ReadFloat64LE() float64

	// WriteTo streams the readable region to w using net.Buffers when w
	// is a *net.TCPConn or *net.UnixConn so the OS can scatter-gather the
	// components in a single writev(2). Other writers receive the
	// components sequentially.
	WriteTo(w io.Writer) (int64, error)

	// Retain increments the reference count.
	Retain() CompositeByteBuf
	// Release decrements the reference count; returns true at zero.
	Release() bool
	// RefCnt reports the current reference count.
	RefCnt() int32
}

type compositeComponent struct {
	data      []byte
	endOffset int // cumulative length up to and including this component
}

type defaultCompositeByteBuf struct {
	components []compositeComponent
	readerIdx  int
	writerIdx  int
	lastHit    int
	refcnt     atomic.Int32
}

// NewCompositeByteBuf creates an empty composite with refcount 1.
func NewCompositeByteBuf(components ...[]byte) CompositeByteBuf {
	c := &defaultCompositeByteBuf{}
	c.refcnt.Store(1)
	for _, bs := range components {
		c.AddComponent(bs)
	}
	return c
}

func (c *defaultCompositeByteBuf) AddComponent(bs []byte) CompositeByteBuf {
	if len(bs) == 0 {
		return c
	}
	end := c.writerIdx + len(bs)
	c.components = append(c.components, compositeComponent{data: bs, endOffset: end})
	c.writerIdx = end
	return c
}

func (c *defaultCompositeByteBuf) AddByteBuf(bb ByteBuf) CompositeByteBuf {
	if bb == nil {
		panic(ErrNilObject)
	}
	return c.AddComponent(bb.Bytes())
}

func (c *defaultCompositeByteBuf) NumComponents() int {
	return len(c.components)
}

func (c *defaultCompositeByteBuf) Component(i int) []byte {
	return c.components[i].data
}

func (c *defaultCompositeByteBuf) ReadableBytes() int {
	return c.writerIdx - c.readerIdx
}

func (c *defaultCompositeByteBuf) ReaderIndex() int {
	return c.readerIdx
}

func (c *defaultCompositeByteBuf) WriterIndex() int {
	return c.writerIdx
}

// locate maps an absolute position in [0, writerIdx) to the component
// containing that byte and its offset within that component. It caches the
// most recent hit so sequential access stays O(1).
func (c *defaultCompositeByteBuf) locate(pos int) (compIdx, offsetIn int) {
	if pos < 0 || pos >= c.writerIdx {
		panic(ErrCompositeOutOfRange)
	}
	i := c.lastHit
	if i >= len(c.components) {
		i = 0
	}
	// Walk forward or backward from the cached index.
	if pos >= c.components[i].endOffset {
		for i < len(c.components) && pos >= c.components[i].endOffset {
			i++
		}
	} else {
		start := 0
		if i > 0 {
			start = c.components[i-1].endOffset
		}
		for i > 0 && pos < start {
			i--
			if i > 0 {
				start = c.components[i-1].endOffset
			} else {
				start = 0
			}
		}
	}
	c.lastHit = i
	startOfComp := 0
	if i > 0 {
		startOfComp = c.components[i-1].endOffset
	}
	return i, pos - startOfComp
}

func (c *defaultCompositeByteBuf) Skip(n int) CompositeByteBuf {
	if n < 0 {
		panic(ErrInsufficientSize)
	}
	if n == 0 {
		return c
	}
	if c.ReadableBytes() < n {
		panic(ErrInsufficientSize)
	}
	c.readerIdx += n
	return c
}

func (c *defaultCompositeByteBuf) ReadByte() (byte, error) {
	if c.readerIdx >= c.writerIdx {
		return 0, ErrInsufficientSize
	}
	compIdx, off := c.locate(c.readerIdx)
	c.readerIdx++
	return c.components[compIdx].data[off], nil
}

func (c *defaultCompositeByteBuf) ReadBytes(n int) []byte {
	if n < 0 {
		panic(ErrInsufficientSize)
	}
	if n == 0 {
		return []byte{}
	}
	if c.ReadableBytes() < n {
		panic(ErrInsufficientSize)
	}
	startCompIdx, startOff := c.locate(c.readerIdx)
	remain := c.components[startCompIdx].data[startOff:]
	if len(remain) >= n {
		c.readerIdx += n
		return remain[:n]
	}
	out := make([]byte, n)
	copied := copy(out, remain)
	for i := startCompIdx + 1; copied < n; i++ {
		copied += copy(out[copied:], c.components[i].data)
	}
	c.readerIdx += n
	return out
}

func (c *defaultCompositeByteBuf) Bytes() []byte {
	readable := c.ReadableBytes()
	if readable == 0 {
		return []byte{}
	}
	startCompIdx, startOff := c.locate(c.readerIdx)
	remainInFirst := len(c.components[startCompIdx].data) - startOff
	if remainInFirst >= readable {
		return c.components[startCompIdx].data[startOff : startOff+readable]
	}
	out := make([]byte, readable)
	copied := copy(out, c.components[startCompIdx].data[startOff:])
	for i := startCompIdx + 1; copied < readable; i++ {
		copied += copy(out[copied:], c.components[i].data)
	}
	return out
}

func (c *defaultCompositeByteBuf) BytesCopy() []byte {
	readable := c.ReadableBytes()
	if readable == 0 {
		return []byte{}
	}
	out := make([]byte, readable)
	startCompIdx, startOff := c.locate(c.readerIdx)
	copied := copy(out, c.components[startCompIdx].data[startOff:])
	for i := startCompIdx + 1; copied < readable; i++ {
		copied += copy(out[copied:], c.components[i].data)
	}
	return out
}

func (c *defaultCompositeByteBuf) DecomposeReadable() [][]byte {
	readable := c.ReadableBytes()
	if readable == 0 {
		return nil
	}
	startCompIdx, startOff := c.locate(c.readerIdx)
	out := make([][]byte, 0, len(c.components)-startCompIdx)
	first := c.components[startCompIdx].data[startOff:]
	if len(first) >= readable {
		out = append(out, first[:readable])
		return out
	}
	out = append(out, first)
	remain := readable - len(first)
	for i := startCompIdx + 1; i < len(c.components) && remain > 0; i++ {
		seg := c.components[i].data
		if len(seg) >= remain {
			out = append(out, seg[:remain])
			remain = 0
			break
		}
		out = append(out, seg)
		remain -= len(seg)
	}
	return out
}

// readMultiByte fetches n bytes into dst[:n], advancing the reader. n must
// be <= len(dst) and <= ReadableBytes.
func (c *defaultCompositeByteBuf) readMultiByte(dst []byte, n int) {
	if c.ReadableBytes() < n {
		panic(ErrInsufficientSize)
	}
	startCompIdx, startOff := c.locate(c.readerIdx)
	first := c.components[startCompIdx].data[startOff:]
	if len(first) >= n {
		copy(dst[:n], first[:n])
		c.readerIdx += n
		return
	}
	copied := copy(dst[:n], first)
	for i := startCompIdx + 1; copied < n; i++ {
		copied += copy(dst[copied:n], c.components[i].data)
	}
	c.readerIdx += n
}

func (c *defaultCompositeByteBuf) ReadUInt16() uint16 {
	var tmp [2]byte
	c.readMultiByte(tmp[:], 2)
	return binary.BigEndian.Uint16(tmp[:])
}

func (c *defaultCompositeByteBuf) ReadUInt32() uint32 {
	var tmp [4]byte
	c.readMultiByte(tmp[:], 4)
	return binary.BigEndian.Uint32(tmp[:])
}

func (c *defaultCompositeByteBuf) ReadUInt64() uint64 {
	var tmp [8]byte
	c.readMultiByte(tmp[:], 8)
	return binary.BigEndian.Uint64(tmp[:])
}

func (c *defaultCompositeByteBuf) ReadInt16() int16 { return int16(c.ReadUInt16()) }
func (c *defaultCompositeByteBuf) ReadInt32() int32 { return int32(c.ReadUInt32()) }
func (c *defaultCompositeByteBuf) ReadInt64() int64 { return int64(c.ReadUInt64()) }
func (c *defaultCompositeByteBuf) ReadFloat32() float32 {
	return math.Float32frombits(c.ReadUInt32())
}
func (c *defaultCompositeByteBuf) ReadFloat64() float64 {
	return math.Float64frombits(c.ReadUInt64())
}

func (c *defaultCompositeByteBuf) ReadUInt16LE() uint16 {
	var tmp [2]byte
	c.readMultiByte(tmp[:], 2)
	return binary.LittleEndian.Uint16(tmp[:])
}

func (c *defaultCompositeByteBuf) ReadUInt32LE() uint32 {
	var tmp [4]byte
	c.readMultiByte(tmp[:], 4)
	return binary.LittleEndian.Uint32(tmp[:])
}

func (c *defaultCompositeByteBuf) ReadUInt64LE() uint64 {
	var tmp [8]byte
	c.readMultiByte(tmp[:], 8)
	return binary.LittleEndian.Uint64(tmp[:])
}

func (c *defaultCompositeByteBuf) ReadInt16LE() int16 { return int16(c.ReadUInt16LE()) }
func (c *defaultCompositeByteBuf) ReadInt32LE() int32 { return int32(c.ReadUInt32LE()) }
func (c *defaultCompositeByteBuf) ReadInt64LE() int64 { return int64(c.ReadUInt64LE()) }
func (c *defaultCompositeByteBuf) ReadFloat32LE() float32 {
	return math.Float32frombits(c.ReadUInt32LE())
}
func (c *defaultCompositeByteBuf) ReadFloat64LE() float64 {
	return math.Float64frombits(c.ReadUInt64LE())
}

// WriteTo exposes the readable components to w. When w participates in the
// io.ReaderFrom scatter-gather contract (net.Conn on Linux/macOS), the
// net.Buffers.WriteTo path lets the OS emit a single writev(2) without
// merging the components beforehand.
func (c *defaultCompositeByteBuf) WriteTo(w io.Writer) (int64, error) {
	segs := c.DecomposeReadable()
	if len(segs) == 0 {
		return 0, nil
	}
	bufs := net.Buffers(segs)
	n, err := bufs.WriteTo(w)
	if n > 0 {
		c.readerIdx += int(n)
	}
	return n, err
}

func (c *defaultCompositeByteBuf) Retain() CompositeByteBuf {
	c.refcnt.Add(1)
	return c
}

func (c *defaultCompositeByteBuf) Release() bool {
	n := c.refcnt.Add(-1)
	if n < 0 {
		panic(ErrRefCountUnderflow)
	}
	return n == 0
}

func (c *defaultCompositeByteBuf) RefCnt() int32 {
	return c.refcnt.Load()
}
