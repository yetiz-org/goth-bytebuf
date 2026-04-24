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

// CompositeByteBuf is a zero-copy ByteBuf that composes a sequence of
// component byte slices. Reads walk the components without copying; writes
// land in an internal writable tail so earlier components stay untouched.
// Components alias their sources, so mutations through the source (within
// the original capacity) remain visible through the composite.
//
// CompositeByteBuf is NOT goroutine-safe.
type CompositeByteBuf interface {
	ByteBuf
	Slicer
	RefCounted
	io.WriterTo

	// AddComponent appends a component that aliases the readable region of
	// bb. The composite does not Retain bb; the caller keeps ownership of
	// bb's lifecycle. Mutations to bb's readable region (within its
	// original capacity) remain visible through the composite.
	AddComponent(bb ByteBuf) CompositeByteBuf

	// AddComponents is the variadic form of AddComponent.
	AddComponents(bbs ...ByteBuf) CompositeByteBuf
}

type compositeComponent struct {
	data      []byte
	endOffset int // cumulative length up to and including this component
}

type defaultCompositeByteBuf struct {
	components      []compositeComponent
	tail            *DefaultByteBuf // when non-nil, last component aliases tail.Bytes()
	readerIdx       int
	writerIdx       int
	prevReaderIdx   int
	prevWriterIdx   int
	lastHit         int
	refcnt          atomic.Int32
}

// Compile-time assertions guarding the ByteBuf / Slicer / RefCounted /
// CompositeByteBuf / io.WriterTo contracts.
var (
	_ ByteBuf          = (*defaultCompositeByteBuf)(nil)
	_ Slicer           = (*defaultCompositeByteBuf)(nil)
	_ RefCounted       = (*defaultCompositeByteBuf)(nil)
	_ CompositeByteBuf = (*defaultCompositeByteBuf)(nil)
	_ io.WriterTo      = (*defaultCompositeByteBuf)(nil)
)

// NewCompositeByteBuf creates an empty composite with refcount 1 and
// appends the provided components.
func NewCompositeByteBuf(bbs ...ByteBuf) CompositeByteBuf {
	c := &defaultCompositeByteBuf{}
	c.refcnt.Store(1)
	return c.AddComponents(bbs...)
}

// AddComponent appends bb's readable region as a new component. When bb is
// itself a CompositeByteBuf, the underlying components are flattened in so
// nested composites do not accumulate.
func (c *defaultCompositeByteBuf) AddComponent(bb ByteBuf) CompositeByteBuf {
	if bb == nil {
		panic(ErrNilObject)
	}
	if sub, ok := bb.(*defaultCompositeByteBuf); ok {
		c.appendFlattened(sub)
		return c
	}
	bs := bb.Bytes()
	if len(bs) == 0 {
		return c
	}
	c.tail = nil
	end := c.writerIdx + len(bs)
	c.components = append(c.components, compositeComponent{data: bs, endOffset: end})
	c.writerIdx = end
	return c
}

// AddComponents applies AddComponent to each provided ByteBuf in order.
func (c *defaultCompositeByteBuf) AddComponents(bbs ...ByteBuf) CompositeByteBuf {
	for _, bb := range bbs {
		if bb == nil {
			panic(ErrNilObject)
		}
		c.AddComponent(bb)
	}
	return c
}

// appendFlattened copies sub's readable component slices (not the bytes
// themselves) into c.
func (c *defaultCompositeByteBuf) appendFlattened(sub *defaultCompositeByteBuf) {
	readable := sub.ReadableBytes()
	if readable == 0 {
		return
	}
	c.tail = nil
	startComp, startOff := sub.locate(sub.readerIdx)
	first := sub.components[startComp].data[startOff:]
	if len(first) >= readable {
		first = first[:readable]
		end := c.writerIdx + len(first)
		c.components = append(c.components, compositeComponent{data: first, endOffset: end})
		c.writerIdx = end
		return
	}
	acc := c.writerIdx + len(first)
	c.components = append(c.components, compositeComponent{data: first, endOffset: acc})
	remain := readable - len(first)
	for i := startComp + 1; i < len(sub.components) && remain > 0; i++ {
		d := sub.components[i].data
		if len(d) >= remain {
			d = d[:remain]
			remain = 0
		} else {
			remain -= len(d)
		}
		acc += len(d)
		c.components = append(c.components, compositeComponent{data: d, endOffset: acc})
	}
	c.writerIdx = acc
}

// locate maps an absolute position in [0, writerIdx) to the component
// containing that byte and its offset within that component. Caches the
// most recent hit so sequential access stays O(1).
func (c *defaultCompositeByteBuf) locate(pos int) (compIdx, offsetIn int) {
	if pos < 0 || pos >= c.writerIdx {
		panic(ErrCompositeOutOfRange)
	}
	i := c.lastHit
	if i >= len(c.components) {
		i = 0
	}
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

// ---------- index / state management ----------

func (c *defaultCompositeByteBuf) ReaderIndex() int { return c.readerIdx }
func (c *defaultCompositeByteBuf) WriterIndex() int { return c.writerIdx }
func (c *defaultCompositeByteBuf) ReadableBytes() int {
	return c.writerIdx - c.readerIdx
}

// Cap reports the total storage capacity: the sum of all component lengths
// plus the spare capacity in the writable tail.
func (c *defaultCompositeByteBuf) Cap() int {
	total := c.writerIdx
	if c.tail != nil {
		total += c.tail.Cap() - c.tail.writerIndex
	}
	return total
}

func (c *defaultCompositeByteBuf) MarkReaderIndex() ByteBuf {
	c.prevReaderIdx = c.readerIdx
	return c
}

func (c *defaultCompositeByteBuf) ResetReaderIndex() ByteBuf {
	c.readerIdx = c.prevReaderIdx
	c.prevReaderIdx = 0
	return c
}

func (c *defaultCompositeByteBuf) MarkWriterIndex() ByteBuf {
	c.prevWriterIdx = c.writerIdx
	return c
}

func (c *defaultCompositeByteBuf) ResetWriterIndex() ByteBuf {
	c.writerIdx = c.prevWriterIdx
	c.prevWriterIdx = 0
	return c
}

// Reset clears all indices (reader, writer, and marks) while keeping the
// components. Use Close to drop components and backing storage.
func (c *defaultCompositeByteBuf) Reset() ByteBuf {
	c.readerIdx = 0
	c.writerIdx = 0
	c.prevReaderIdx = 0
	c.prevWriterIdx = 0
	c.lastHit = 0
	c.components = nil
	c.tail = nil
	return c
}

// Close drops every component and the writable tail, and zeroes all
// indices. RefCnt is unaffected; refcount management is via Retain/Release.
func (c *defaultCompositeByteBuf) Close() error {
	c.components = nil
	c.tail = nil
	c.readerIdx = 0
	c.writerIdx = 0
	c.prevReaderIdx = 0
	c.prevWriterIdx = 0
	c.lastHit = 0
	return nil
}

func (c *defaultCompositeByteBuf) Skip(v int) ByteBuf {
	if v < 0 {
		panic(ErrInsufficientSize)
	}
	if v == 0 {
		return c
	}
	if c.ReadableBytes() < v {
		panic(ErrInsufficientSize)
	}
	c.readerIdx += v
	return c
}

// ---------- zero-copy query ----------

// Bytes returns a mutable view of the readable region. When the region
// spans multiple components, those components are consolidated into one
// first so the returned slice honors the mutable-view contract on
// subsequent calls as well.
func (c *defaultCompositeByteBuf) Bytes() []byte {
	readable := c.ReadableBytes()
	if readable == 0 {
		return []byte{}
	}
	startComp, startOff := c.locate(c.readerIdx)
	remainInFirst := len(c.components[startComp].data) - startOff
	if remainInFirst >= readable {
		return c.components[startComp].data[startOff : startOff+readable]
	}
	// Lazy consolidate from startComp onwards into a single component.
	merged := make([]byte, readable)
	copied := copy(merged, c.components[startComp].data[startOff:])
	for i := startComp + 1; copied < readable; i++ {
		copied += copy(merged[copied:], c.components[i].data)
	}
	newComponents := make([]compositeComponent, 0, startComp+2)
	newComponents = append(newComponents, c.components[:startComp]...)
	if startOff > 0 {
		// Keep a placeholder so offsets align: the trimmed prefix of the
		// old startComp still needs to account for the already-consumed
		// bytes ahead of readerIdx.
		newComponents = append(newComponents, compositeComponent{
			data:      c.components[startComp].data[:startOff],
			endOffset: c.readerIdx,
		})
	}
	newComponents = append(newComponents, compositeComponent{
		data:      merged,
		endOffset: c.readerIdx + readable,
	})
	c.components = newComponents
	c.tail = nil
	c.lastHit = 0
	return merged
}

// BytesCopy returns an independent copy of the readable region and does
// not mutate internal state.
func (c *defaultCompositeByteBuf) BytesCopy() []byte {
	readable := c.ReadableBytes()
	if readable == 0 {
		return []byte{}
	}
	out := make([]byte, readable)
	startComp, startOff := c.locate(c.readerIdx)
	copied := copy(out, c.components[startComp].data[startOff:])
	for i := startComp + 1; copied < readable; i++ {
		copied += copy(out[copied:], c.components[i].data)
	}
	return out
}

// decomposeReadable returns the component slices covering the readable
// region in order. It is used internally by WriteTo to hand segments to
// net.Buffers.WriteTo without an intermediate copy.
func (c *defaultCompositeByteBuf) decomposeReadable() [][]byte {
	readable := c.ReadableBytes()
	if readable == 0 {
		return nil
	}
	startComp, startOff := c.locate(c.readerIdx)
	out := make([][]byte, 0, len(c.components)-startComp)
	first := c.components[startComp].data[startOff:]
	if len(first) >= readable {
		out = append(out, first[:readable])
		return out
	}
	out = append(out, first)
	remain := readable - len(first)
	for i := startComp + 1; i < len(c.components) && remain > 0; i++ {
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

// Compact drops components whose bytes are fully consumed and trims the
// front of the first partially-consumed component. The readable region is
// preserved; readerIdx resets to 0 and marked indices shift accordingly.
func (c *defaultCompositeByteBuf) Compact() ByteBuf {
	if c.readerIdx == 0 {
		return c
	}
	shift := c.readerIdx
	if c.readerIdx >= c.writerIdx {
		// Everything consumed: drop all components.
		c.components = nil
		c.tail = nil
		c.readerIdx = 0
		c.writerIdx = 0
		c.prevReaderIdx = 0
		c.prevWriterIdx = 0
		c.lastHit = 0
		return c
	}
	compIdx, offsetIn := c.locate(c.readerIdx)
	c.components = append(c.components[:0], c.components[compIdx:]...)
	if offsetIn > 0 {
		c.components[0].data = c.components[0].data[offsetIn:]
	}
	for i := range c.components {
		c.components[i].endOffset -= shift
	}
	c.writerIdx -= shift
	c.readerIdx = 0
	if c.prevReaderIdx > 0 {
		if c.prevReaderIdx >= shift {
			c.prevReaderIdx -= shift
		} else {
			c.prevReaderIdx = 0
		}
	}
	if c.prevWriterIdx > 0 {
		if c.prevWriterIdx >= shift {
			c.prevWriterIdx -= shift
		} else {
			c.prevWriterIdx = 0
		}
	}
	c.lastHit = 0
	return c
}

// Clone returns an independent DefaultByteBuf holding a flattened copy of
// the readable region. Callers can mutate the clone without affecting the
// composite or its sources.
func (c *defaultCompositeByteBuf) Clone() ByteBuf {
	readable := c.ReadableBytes()
	if readable == 0 {
		return EmptyByteBuf()
	}
	clone := newDefaultByteBuf()
	clone.buf = c.BytesCopy()
	clone.writerIndex = readable
	return clone
}

// Grow increases the writable capacity by v bytes. It ensures the internal
// tail has at least v additional bytes of spare capacity.
func (c *defaultCompositeByteBuf) Grow(v int) ByteBuf {
	if v <= 0 {
		return c
	}
	c.ensureTail()
	c.tail.Grow(v)
	c.syncTailAfterWrite(0)
	return c
}

// EnsureCapacity guarantees at least n bytes of writable space. Write
// throughput benefits when the caller sizes the tail up front.
func (c *defaultCompositeByteBuf) EnsureCapacity(n int) ByteBuf {
	if n < 0 {
		panic(ErrInsufficientSize)
	}
	if n == 0 {
		return c
	}
	spare := 0
	if c.tail != nil {
		spare = c.tail.Cap() - c.tail.writerIndex
	}
	if spare >= n {
		return c
	}
	c.ensureTail()
	c.tail.EnsureCapacity(n)
	c.syncTailAfterWrite(0)
	return c
}

// ---------- writable tail machinery ----------

// ensureTail prepares a writable tail component. If the current tail is
// sealed (nil), a fresh *DefaultByteBuf is attached as the last component.
func (c *defaultCompositeByteBuf) ensureTail() {
	if c.tail != nil {
		return
	}
	c.tail = newDefaultByteBuf()
	c.components = append(c.components, compositeComponent{
		data:      nil,
		endOffset: c.writerIdx,
	})
}

// syncTailAfterWrite re-aliases the last component's data against the
// current tail.Bytes() view and advances the composite writerIdx by delta.
// Must be called after every tail mutation because DefaultByteBuf.prepare
// may have reallocated the backing array.
func (c *defaultCompositeByteBuf) syncTailAfterWrite(delta int) {
	last := len(c.components) - 1
	c.components[last].data = c.tail.buf[:c.tail.writerIndex]
	c.components[last].endOffset += delta
	c.writerIdx += delta
}

// tailWrite executes fn on the tail, re-aliases the last component, and
// advances writerIdx by the number of bytes fn wrote.
func (c *defaultCompositeByteBuf) tailWrite(fn func(t *DefaultByteBuf)) {
	c.ensureTail()
	before := c.tail.writerIndex
	fn(c.tail)
	c.syncTailAfterWrite(c.tail.writerIndex - before)
}

// ---------- Write / io.Writer / io.WriterAt ----------

func (c *defaultCompositeByteBuf) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.tailWrite(func(t *DefaultByteBuf) {
		_, _ = t.Write(p)
	})
	return len(p), nil
}

// WriteAt writes p at the absolute offset. Writes into the range already
// covered by existing components alias those components' backing arrays;
// writes past writerIdx extend the writable tail.
func (c *defaultCompositeByteBuf) WriteAt(p []byte, offset int64) (n int, err error) {
	pl := len(p)
	if pl == 0 {
		return 0, nil
	}
	maxInt := int(^uint(0) >> 1)
	if offset < 0 || offset > int64(maxInt-pl) {
		panic(ErrInsufficientSize)
	}
	off := int(offset)

	// Region beyond writerIdx: fill any gap with zeros via the tail, then
	// append p.
	if off >= c.writerIdx {
		c.tailWrite(func(t *DefaultByteBuf) {
			gap := off - c.writerIdx
			for gap > 0 {
				// Write gap in bounded chunks so a hostile offset cannot
				// request a GB-scale zero buffer in one shot.
				chunk := gap
				const zeroChunk = 4096
				if chunk > zeroChunk {
					chunk = zeroChunk
				}
				var zeros [zeroChunk]byte
				_, _ = t.Write(zeros[:chunk])
				gap -= chunk
			}
			_, _ = t.Write(p)
		})
		return pl, nil
	}

	// Overlap with existing components: write through their backing arrays.
	written := 0
	pos := off
	for written < pl && pos < c.writerIdx {
		compIdx, offIn := c.locate(pos)
		avail := len(c.components[compIdx].data) - offIn
		chunk := pl - written
		if chunk > avail {
			chunk = avail
		}
		copy(c.components[compIdx].data[offIn:offIn+chunk], p[written:written+chunk])
		written += chunk
		pos += chunk
	}
	// Tail of p beyond writerIdx: extend.
	if written < pl {
		remain := p[written:]
		c.tailWrite(func(t *DefaultByteBuf) {
			_, _ = t.Write(remain)
		})
	}
	return pl, nil
}

func (c *defaultCompositeByteBuf) AppendByte(b byte) ByteBuf {
	c.tailWrite(func(t *DefaultByteBuf) { t.AppendByte(b) })
	return c
}

func (c *defaultCompositeByteBuf) WriteByte(b byte) error {
	c.tailWrite(func(t *DefaultByteBuf) { _ = t.WriteByte(b) })
	return nil
}

func (c *defaultCompositeByteBuf) WriteBytes(bs []byte) ByteBuf {
	if len(bs) == 0 {
		return c
	}
	c.tailWrite(func(t *DefaultByteBuf) { t.WriteBytes(bs) })
	return c
}

func (c *defaultCompositeByteBuf) WriteString(s string) ByteBuf {
	if len(s) == 0 {
		return c
	}
	c.tailWrite(func(t *DefaultByteBuf) { t.WriteString(s) })
	return c
}

func (c *defaultCompositeByteBuf) WriteByteBuf(buf ByteBuf) ByteBuf {
	if buf == nil {
		panic(ErrNilObject)
	}
	c.WriteBytes(buf.Bytes())
	return c
}

func (c *defaultCompositeByteBuf) WriteReader(reader io.Reader) ByteBuf {
	if reader == nil {
		panic(ErrNilObject)
	}
	c.tailWrite(func(t *DefaultByteBuf) { t.WriteReader(reader) })
	return c
}

func (c *defaultCompositeByteBuf) WriteInt16(v int16) ByteBuf  { c.WriteUInt16(uint16(v)); return c }
func (c *defaultCompositeByteBuf) WriteInt32(v int32) ByteBuf  { c.WriteUInt32(uint32(v)); return c }
func (c *defaultCompositeByteBuf) WriteInt64(v int64) ByteBuf  { c.WriteUInt64(uint64(v)); return c }
func (c *defaultCompositeByteBuf) WriteInt16LE(v int16) ByteBuf { c.WriteUInt16LE(uint16(v)); return c }
func (c *defaultCompositeByteBuf) WriteInt32LE(v int32) ByteBuf { c.WriteUInt32LE(uint32(v)); return c }
func (c *defaultCompositeByteBuf) WriteInt64LE(v int64) ByteBuf { c.WriteUInt64LE(uint64(v)); return c }

func (c *defaultCompositeByteBuf) WriteUInt16(v uint16) ByteBuf {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return c.WriteBytes(b[:])
}

func (c *defaultCompositeByteBuf) WriteUInt32(v uint32) ByteBuf {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return c.WriteBytes(b[:])
}

func (c *defaultCompositeByteBuf) WriteUInt64(v uint64) ByteBuf {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return c.WriteBytes(b[:])
}

func (c *defaultCompositeByteBuf) WriteFloat32(v float32) ByteBuf {
	return c.WriteUInt32(math.Float32bits(v))
}

func (c *defaultCompositeByteBuf) WriteFloat64(v float64) ByteBuf {
	return c.WriteUInt64(math.Float64bits(v))
}

func (c *defaultCompositeByteBuf) WriteUInt16LE(v uint16) ByteBuf {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	return c.WriteBytes(b[:])
}

func (c *defaultCompositeByteBuf) WriteUInt32LE(v uint32) ByteBuf {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return c.WriteBytes(b[:])
}

func (c *defaultCompositeByteBuf) WriteUInt64LE(v uint64) ByteBuf {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return c.WriteBytes(b[:])
}

func (c *defaultCompositeByteBuf) WriteFloat32LE(v float32) ByteBuf {
	return c.WriteUInt32LE(math.Float32bits(v))
}

func (c *defaultCompositeByteBuf) WriteFloat64LE(v float64) ByteBuf {
	return c.WriteUInt64LE(math.Float64bits(v))
}

// ---------- Read / io.Reader ----------

func (c *defaultCompositeByteBuf) Read(p []byte) (n int, err error) {
	if c.ReadableBytes() == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	toCopy := c.ReadableBytes()
	if toCopy > len(p) {
		toCopy = len(p)
	}
	startComp, startOff := c.locate(c.readerIdx)
	first := c.components[startComp].data[startOff:]
	copied := copy(p, first[:min(len(first), toCopy)])
	for i := startComp + 1; copied < toCopy; i++ {
		copied += copy(p[copied:toCopy], c.components[i].data)
	}
	c.readerIdx += toCopy
	return toCopy, nil
}

func (c *defaultCompositeByteBuf) MustReadByte() byte {
	b, err := c.ReadByte()
	if err != nil {
		panic(ErrInsufficientSize)
	}
	return b
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
	startComp, startOff := c.locate(c.readerIdx)
	remain := c.components[startComp].data[startOff:]
	if len(remain) >= n {
		c.readerIdx += n
		return remain[:n]
	}
	out := make([]byte, n)
	copied := copy(out, remain)
	for i := startComp + 1; copied < n; i++ {
		copied += copy(out[copied:], c.components[i].data)
	}
	c.readerIdx += n
	return out
}

func (c *defaultCompositeByteBuf) ReadByteBuf(n int) ByteBuf {
	bs := c.ReadBytes(n)
	buf := newDefaultByteBuf()
	buf.WriteBytes(bs)
	return buf
}

func (c *defaultCompositeByteBuf) ReadWriter(writer io.Writer) ByteBuf {
	segs := c.decomposeReadable()
	if len(segs) == 0 {
		return c
	}
	written := int64(0)
	for _, seg := range segs {
		n, err := writer.Write(seg)
		written += int64(n)
		if err != nil {
			c.readerIdx += int(written)
			panic(err)
		}
	}
	c.readerIdx += int(written)
	return c
}

// readMultiByte fetches n bytes into dst[:n], advancing the reader.
func (c *defaultCompositeByteBuf) readMultiByte(dst []byte, n int) {
	if c.ReadableBytes() < n {
		panic(ErrInsufficientSize)
	}
	startComp, startOff := c.locate(c.readerIdx)
	first := c.components[startComp].data[startOff:]
	if len(first) >= n {
		copy(dst[:n], first[:n])
		c.readerIdx += n
		return
	}
	copied := copy(dst[:n], first)
	for i := startComp + 1; copied < n; i++ {
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

// ---------- Slicer ----------

// Slice returns a sub-composite view over [from, from+length) within the
// readable region. The view shares component storage with the parent and
// is itself a ByteBuf.
func (c *defaultCompositeByteBuf) Slice(from, length int) ByteBuf {
	if from < 0 || length < 0 || from+length > c.ReadableBytes() {
		panic(ErrInsufficientSize)
	}
	sub := &defaultCompositeByteBuf{}
	sub.refcnt.Store(1)
	if length == 0 {
		return sub
	}
	startAbs := c.readerIdx + from
	endAbs := startAbs + length
	startComp, startOffIn := c.locate(startAbs)
	endComp, endOffInIncl := c.locate(endAbs - 1)
	if startComp == endComp {
		data := c.components[startComp].data[startOffIn : endOffInIncl+1]
		sub.components = []compositeComponent{{data: data, endOffset: length}}
		sub.writerIdx = length
		return sub
	}
	acc := 0
	firstData := c.components[startComp].data[startOffIn:]
	acc += len(firstData)
	sub.components = append(sub.components, compositeComponent{data: firstData, endOffset: acc})
	for i := startComp + 1; i < endComp; i++ {
		d := c.components[i].data
		acc += len(d)
		sub.components = append(sub.components, compositeComponent{data: d, endOffset: acc})
	}
	lastData := c.components[endComp].data[:endOffInIncl+1]
	acc += len(lastData)
	sub.components = append(sub.components, compositeComponent{data: lastData, endOffset: acc})
	sub.writerIdx = acc
	return sub
}

// Duplicate returns an independent view that shares components with c but
// has its own reader/writer and mark indices.
func (c *defaultCompositeByteBuf) Duplicate() ByteBuf {
	sub := &defaultCompositeByteBuf{}
	sub.refcnt.Store(1)
	sub.components = append([]compositeComponent(nil), c.components...)
	sub.readerIdx = c.readerIdx
	sub.writerIdx = c.writerIdx
	return sub
}

// ReadSlice advances readerIdx by n and returns a sub-composite covering
// the consumed bytes.
func (c *defaultCompositeByteBuf) ReadSlice(n int) ByteBuf {
	if n < 0 {
		panic(ErrInsufficientSize)
	}
	if c.ReadableBytes() < n {
		panic(ErrInsufficientSize)
	}
	view := c.Slice(0, n)
	c.readerIdx += n
	return view
}

// ---------- RefCounted ----------

func (c *defaultCompositeByteBuf) Retain() ByteBuf {
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

// ---------- io.WriterTo ----------

// WriteTo drains the readable region to w. When w is a *net.TCPConn or
// *net.UnixConn, the net.Buffers.WriteTo fast path collapses the
// components into a single writev(2) syscall without pre-merging.
func (c *defaultCompositeByteBuf) WriteTo(w io.Writer) (int64, error) {
	segs := c.decomposeReadable()
	if len(segs) == 0 {
		return 0, nil
	}
	switch w.(type) {
	case *net.TCPConn, *net.UnixConn:
		bufs := net.Buffers(segs)
		n, err := bufs.WriteTo(w)
		if n > 0 {
			c.readerIdx += int(n)
		}
		return n, err
	}
	var total int64
	for _, seg := range segs {
		n, err := w.Write(seg)
		total += int64(n)
		if err != nil {
			c.readerIdx += int(total)
			return total, err
		}
	}
	c.readerIdx += int(total)
	return total, nil
}
