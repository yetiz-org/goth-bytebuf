package buf

import "sync"

// poolClasses is the ascending list of backing-array sizes the pool tracks.
// Requests with minCap larger than the biggest class bypass the pool.
var poolClasses = [...]int{64, 256, 1 << 10, 4 << 10, 16 << 10, 64 << 10, 256 << 10}

var pools [len(poolClasses)]sync.Pool

func init() {
	for i := range poolClasses {
		size := poolClasses[i]
		classIdx := int32(i)
		pools[i] = sync.Pool{
			New: func() any {
				b := newDefaultByteBuf()
				b.buf = make([]byte, size)
				b.poolIdx = classIdx
				return b
			},
		}
	}
}

// poolClassIndex returns the index of the smallest class >= minCap, or -1
// if minCap exceeds the largest class.
func poolClassIndex(minCap int) int {
	for i, size := range poolClasses {
		if size >= minCap {
			return i
		}
	}
	return -1
}

// AcquireByteBuf returns a buffer with Cap() >= minCap. The buffer has
// refcount 1, zeroed indices, and an unspecified readable content. Buffers
// obtained via AcquireByteBuf must be returned with ReleaseByteBuf when the
// caller is done.
func AcquireByteBuf(minCap int) ByteBuf {
	if minCap < 0 {
		panic(ErrInsufficientSize)
	}
	if minCap == 0 {
		minCap = poolClasses[0]
	}
	idx := poolClassIndex(minCap)
	if idx < 0 {
		b := newDefaultByteBuf()
		b.buf = make([]byte, minCap)
		return b
	}
	b := pools[idx].Get().(*DefaultByteBuf)
	b.refcnt.Store(1)
	b.readerIndex = 0
	b.writerIndex = 0
	b.prevReaderIndex = 0
	b.prevWriterIndex = 0
	b.poolIdx = int32(idx)
	// Replace the backing array when it no longer matches the class size,
	// which can happen if the pooled buffer's buf was detached by a grow.
	if cap(b.buf) != poolClasses[idx] {
		b.buf = make([]byte, poolClasses[idx])
	}
	return b
}

// ReleaseByteBuf returns bb to its originating pool only when bb carries
// a valid poolIdx and owns a class-sized backing array. Views created by
// Slice, Duplicate, or ReadSlice carry poolIdx == -1 and are never pooled.
// Buffers whose backing array no longer matches the class size are dropped
// so the pool caches only predictably-sized arrays. A nil or
// non-*DefaultByteBuf argument is a no-op.
func ReleaseByteBuf(bb ByteBuf) {
	if bb == nil {
		return
	}
	b, ok := bb.(*DefaultByteBuf)
	if !ok {
		return
	}
	idx := b.poolIdx
	if idx < 0 || int(idx) >= len(poolClasses) {
		return
	}
	if b.buf == nil || cap(b.buf) != poolClasses[idx] {
		b.poolIdx = -1
		return
	}
	b.readerIndex = 0
	b.writerIndex = 0
	b.prevReaderIndex = 0
	b.prevWriterIndex = 0
	b.refcnt.Store(0)
	pools[idx].Put(b)
}
