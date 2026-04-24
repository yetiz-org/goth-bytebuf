package buf

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// Reset must keep the backing array; only indices reset.
func TestReset_PreservesCapacity(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("hello world")
	capBefore := b.Cap()
	assert.Greater(t, capBefore, 0)

	b.Reset()
	assert.Equal(t, 0, b.ReadableBytes())
	assert.Equal(t, 0, b.ReaderIndex())
	assert.Equal(t, 0, b.WriterIndex())
	assert.Equal(t, capBefore, b.Cap())
}

// Close must drop the backing array and zero Cap().
func TestClose_StillReleasesStorage(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("payload")
	assert.Greater(t, b.Cap(), 0)

	assert.NoError(t, b.Close())
	assert.Equal(t, 0, b.Cap())
	assert.Nil(t, b.buf)
}

// Writes that fit the preserved capacity must reuse the same backing array.
func TestReuse_AfterReset_NoRealloc(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("0123456789")
	firstPtr := unsafe.SliceData(b.buf)

	for range 50 {
		b.Reset()
		b.WriteString("0123456789")
		assert.Equal(t, firstPtr, unsafe.SliceData(b.buf))
	}
}

// Skip must be allocation-free.
func TestSkip_ZeroAllocation(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteBytes(bytes.Repeat([]byte("x"), 1<<20))

	allocs := testing.AllocsPerRun(1000, func() {
		b.(*DefaultByteBuf).readerIndex = 0
		b.Skip(512)
	})
	assert.Equal(t, 0.0, allocs)
}

// Skip boundary behavior: zero is a no-op, negative panics, overshoot panics.
func TestSkip_Boundary_Conditions(t *testing.T) {
	t.Run("skip_zero_is_noop", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("abcdef")
		r := b.ReaderIndex()
		b.Skip(0)
		assert.Equal(t, r, b.ReaderIndex())
		assert.Equal(t, "abcdef", string(b.Bytes()))
	})

	t.Run("negative_skip_panics", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("abc")
		assert.Panics(t, func() { b.Skip(-1) })
	})

	t.Run("overshoot_panics", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("abc")
		assert.Panics(t, func() { b.Skip(10) })
	})
}

// A single large write into an empty buffer must perform at most a constant
// number of heap allocations independent of payload size.
func TestPrepare_SingleAllocation_LargePayload(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1<<20)

	_ = EmptyByteBuf()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	b := EmptyByteBuf()
	b.WriteBytes(payload)

	runtime.ReadMemStats(&after)

	delta := after.Mallocs - before.Mallocs
	assert.LessOrEqual(t, delta, uint64(6),
		"large write should collapse into a handful of allocations, got %d", delta)

	assert.Equal(t, len(payload), b.ReadableBytes())
}

// EnsureCapacity reaches the requested writable size with the readable
// region compacted to index 0.
func TestEnsureCapacity_SingleGrowPath(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("seed")
	b.EnsureCapacity(4 * 1024 * 1024)
	assert.GreaterOrEqual(t, b.Cap(), b.ReadableBytes()+4*1024*1024)
	assert.Equal(t, 0, b.ReaderIndex())
	assert.Equal(t, "seed", string(b.Bytes()))
}

// rampReader emits a predictable byte ramp so direct-write can be verified
// byte-for-byte.
type rampReader struct {
	total int
	pos   int
}

func (r *rampReader) Read(p []byte) (int, error) {
	if r.pos >= r.total {
		return 0, io.EOF
	}
	n := len(p)
	if remain := r.total - r.pos; n > remain {
		n = remain
	}
	for i := range n {
		p[i] = byte((r.pos + i) & 0xff)
	}
	r.pos += n
	return n, nil
}

// WriteReader must land the reader's bytes in the buffer's own storage,
// across multiple chunk boundaries.
func TestWriteReader_DirectWrite(t *testing.T) {
	total := 9 * writeReaderChunk
	b := EmptyByteBuf()
	b.WriteReader(&rampReader{total: total})
	bs := b.Bytes()
	assert.Equal(t, total, len(bs))
	for i := range total {
		assert.Equal(t, byte(i&0xff), bs[i])
	}
}

// WriteReader must not allocate a per-call scratch buffer when the
// destination has enough capacity and the reader is reused.
func TestWriteReader_NoPerCallTempBuffer(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 8*1024)
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.EnsureCapacity(16 * 1024)

	reader := bytes.NewReader(payload)
	allocs := testing.AllocsPerRun(50, func() {
		b.Reset()
		reader.Reset(payload)
		b.WriteReader(reader)
	})
	assert.Equal(t, 0.0, allocs)
	assert.Equal(t, len(payload), b.ReadableBytes())
}

// An empty reader must not grow the buffer.
func TestWriteReader_EmptyReaderStopsImmediately(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteReader(bytes.NewReader(nil))
	assert.Equal(t, 0, b.ReadableBytes())
}

// Grow(v) keeps its public contract: capacity increases by at least v and
// readable data is preserved.
func TestGrow_PublicAPI_StillAddsCapacity(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("abc")
	cap1 := b.Cap()
	b.Grow(256)
	assert.GreaterOrEqual(t, b.Cap(), cap1+256)
	assert.Equal(t, "abc", string(b.Bytes()))
}

// When prepare reallocates, the readable region is relocated to index 0.
func TestPrepareGrow_CompactsReadableRegion(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("ABCDEFGH")
	_ = b.ReadBytes(5)

	large := bytes.Repeat([]byte{'Z'}, 10_000)
	b.WriteBytes(large)

	assert.Equal(t, 0, b.ReaderIndex())
	bs := b.Bytes()
	assert.Equal(t, "FGH", string(bs[:3]))
	assert.Equal(t, 3+10_000, b.ReadableBytes())
}

// Long write/read/Reset cycles must keep capacity bounded by the payload.
func TestLongLivedReuse_NoUnboundedGrowth(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	payload := []byte(strings.Repeat("x", 512))

	for range 10_000 {
		b.WriteBytes(payload)
		_ = b.ReadBytes(len(payload))
		b.Reset()
	}

	assert.LessOrEqual(t, b.Cap(), 1024)
}
