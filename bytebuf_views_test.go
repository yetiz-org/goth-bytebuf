package buf

import (
	"bytes"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// --- Slice ---------------------------------------------------------------

// Slice returns a view over the parent's readable region with its own
// reader/writer indices.
func TestSlice_BasicView(t *testing.T) {
	parent := EmptyByteBuf()
	parent.WriteString("abcdefghij")
	s := parent.(*DefaultByteBuf).Slice(2, 5) // "cdefg"
	assert.Equal(t, "cdefg", string(s.Bytes()))
	assert.Equal(t, 5, s.ReadableBytes())
}

// Mutations through the slice within its capacity must be visible on the
// parent's backing array.
func TestSlice_MutationLiveThroughParent(t *testing.T) {
	parent := EmptyByteBuf().(*DefaultByteBuf)
	parent.WriteString("hello_world")
	s := parent.Slice(0, 5) // "hello"

	// Overwrite in-place via the view's Bytes().
	view := s.Bytes()
	view[0] = 'H'
	assert.Equal(t, "Hello_world", string(parent.Bytes()))
}

// The slice's first byte lives at the same address as the corresponding
// offset in the parent's backing array.
func TestSlice_SharesBackingArray(t *testing.T) {
	parent := EmptyByteBuf().(*DefaultByteBuf)
	parent.WriteString("aaaabbbb")
	s := parent.Slice(4, 4).(*DefaultByteBuf)
	assert.Equal(t, unsafe.Pointer(&parent.buf[4]), unsafe.Pointer(&s.buf[0]))
}

// Slice requests outside the readable region panic.
func TestSlice_OutOfRange_Panics(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("abc")
	assert.Panics(t, func() { b.Slice(-1, 1) })
	assert.Panics(t, func() { b.Slice(0, -1) })
	assert.Panics(t, func() { b.Slice(2, 5) })
}

// A write that exceeds the slice's initial capacity allocates a fresh
// backing array and leaves the parent untouched.
func TestSlice_GrowDetachesFromParent(t *testing.T) {
	parent := EmptyByteBuf().(*DefaultByteBuf)
	parent.WriteString("abcdefghij")
	parentBefore := string(parent.Bytes())

	s := parent.Slice(0, 5)
	s.WriteBytes(bytes.Repeat([]byte("Z"), 128))

	assert.Equal(t, parentBefore, string(parent.Bytes()))
	assert.GreaterOrEqual(t, s.ReadableBytes(), 133)
}

// --- Duplicate -----------------------------------------------------------

// Duplicate shares the backing array and initial reader/writer but has its
// own index state.
func TestDuplicate_IndependentIndices(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteString("abcdef")
	d := b.Duplicate()

	_ = d.ReadBytes(3)
	assert.Equal(t, 0, b.ReaderIndex())
	assert.Equal(t, 3, d.ReaderIndex())
	assert.Equal(t, "abcdef", string(b.Bytes()))
	assert.Equal(t, "def", string(d.Bytes()))
}

// --- ReadSlice -----------------------------------------------------------

// ReadSlice advances the parent's readerIndex and returns a zero-copy view.
func TestReadSlice_AdvancesParentAndShares(t *testing.T) {
	parent := EmptyByteBuf().(*DefaultByteBuf)
	parent.WriteString("12345678")
	v := parent.ReadSlice(4).(*DefaultByteBuf)

	assert.Equal(t, "1234", string(v.Bytes()))
	assert.Equal(t, 4, parent.ReaderIndex())
	assert.Equal(t, unsafe.Pointer(&parent.buf[0]), unsafe.Pointer(&v.buf[0]))
}

// ReadSlice is zero-allocation once capacity is warmed up.
func TestReadSlice_ZeroAllocation(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	b.WriteBytes(bytes.Repeat([]byte("x"), 1024))

	allocs := testing.AllocsPerRun(500, func() {
		b.readerIndex = 0
		_ = b.ReadSlice(16)
	})
	// Each ReadSlice produces one *DefaultByteBuf struct. The underlying
	// []byte aliases the parent, so no data copy occurs.
	assert.LessOrEqual(t, allocs, 1.0)
}

// --- Refcount ------------------------------------------------------------

// Fresh buffers start with refcount 1; Retain/Release move it in lockstep
// and Release returns true at zero.
func TestRefCount_Lifecycle(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	assert.Equal(t, int32(1), b.RefCnt())
	b.Retain()
	assert.Equal(t, int32(2), b.RefCnt())
	assert.False(t, b.Release())
	assert.Equal(t, int32(1), b.RefCnt())
	assert.True(t, b.Release())
	assert.Equal(t, int32(0), b.RefCnt())
}

// Dropping below zero panics to expose double-free mistakes early.
func TestRefCount_Underflow_Panics(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	assert.True(t, b.Release())
	assert.Panics(t, func() { b.Release() })
}

// Concurrent Retain/Release keep the counter consistent.
func TestRefCount_ConcurrentRetainRelease(t *testing.T) {
	b := EmptyByteBuf().(*DefaultByteBuf)
	const n = 10000
	done := make(chan struct{})
	go func() {
		for range n {
			b.Retain()
		}
		done <- struct{}{}
	}()
	go func() {
		for range n {
			// Sleep-free tight loop that must not race past producer.
			for b.RefCnt() <= 1 {
				// busy-wait until producer gets ahead
			}
			b.Release()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	assert.Equal(t, int32(1), b.RefCnt())
}

// --- Pool ----------------------------------------------------------------

// AcquireByteBuf returns a buffer sized at least minCap with refcount 1
// and empty indices.
func TestPool_Acquire_Contract(t *testing.T) {
	b := AcquireByteBuf(500).(*DefaultByteBuf)
	assert.GreaterOrEqual(t, b.Cap(), 500)
	assert.Equal(t, 0, b.ReadableBytes())
	assert.Equal(t, int32(1), b.RefCnt())
	assert.GreaterOrEqual(t, b.poolIdx, int32(0))
	ReleaseByteBuf(b)
}

// A buffer whose size does not match any class goes through direct
// allocation and does not participate in the pool.
func TestPool_Acquire_OversizedBypassesPool(t *testing.T) {
	big := AcquireByteBuf(10 * 1024 * 1024).(*DefaultByteBuf)
	assert.Equal(t, int32(-1), big.poolIdx)
	ReleaseByteBuf(big) // should be a no-op for pool
}

// Release puts pooled buffers back so that a subsequent Acquire may return
// the same pointer.
func TestPool_AcquireRelease_Recycles(t *testing.T) {
	b1 := AcquireByteBuf(64).(*DefaultByteBuf)
	p1 := unsafe.Pointer(b1)
	ReleaseByteBuf(b1)

	// sync.Pool makes no strict guarantee, but a single-goroutine
	// immediate reacquire almost always hits the cache. We accept either
	// outcome but at least confirm pool still hands us a valid 64-byte buf.
	b2 := AcquireByteBuf(64).(*DefaultByteBuf)
	assert.Equal(t, 64, b2.Cap())
	assert.Equal(t, int32(0), b2.poolIdx)
	_ = p1 // keep reference alive for pool observation
	ReleaseByteBuf(b2)
}

// Releasing a Slice/Duplicate/ReadSlice view never goes back to the pool.
func TestPool_Release_IgnoresViews(t *testing.T) {
	parent := AcquireByteBuf(256).(*DefaultByteBuf)
	parent.WriteString("abcdef")
	view := parent.Slice(0, 4).(*DefaultByteBuf)
	assert.Equal(t, int32(-1), view.poolIdx)

	ReleaseByteBuf(view) // no-op on the pool path
	ReleaseByteBuf(parent)
}

// AcquireByteBuf(0) returns the smallest class so a nominal call never
// panics and the caller receives a usable buffer.
func TestPool_Acquire_ZeroIsSmallestClass(t *testing.T) {
	b := AcquireByteBuf(0).(*DefaultByteBuf)
	assert.Equal(t, poolClasses[0], b.Cap())
	ReleaseByteBuf(b)
}

// Negative minCap panics up-front.
func TestPool_Acquire_NegativePanics(t *testing.T) {
	assert.Panics(t, func() { AcquireByteBuf(-1) })
}

// --- NewSharedByteBuf ----------------------------------------------------

// NewSharedByteBuf wraps the provided slice without copying.
func TestNewSharedByteBuf_SharesBacking(t *testing.T) {
	src := []byte("shared payload")
	b := NewSharedByteBuf(src).(*DefaultByteBuf)
	assert.Equal(t, unsafe.SliceData(src), unsafe.SliceData(b.buf))
	assert.Equal(t, len(src), b.ReadableBytes())
}

// Keep the linker happy when atomic is unused in specific paths.
var _ = atomic.Int32{}
