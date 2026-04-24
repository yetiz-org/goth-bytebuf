package buf

import (
	"bytes"
	"io"
	"testing"
)

// BenchmarkFreshBuf_WriteLarge_1MB allocates a new buffer on every iteration
// and writes 1MB into it, measuring the allocations charged to the write.
func BenchmarkFreshBuf_WriteLarge_1MB(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1<<20)
	b.ReportAllocs()
	for b.Loop() {
		buf := EmptyByteBuf()
		buf.WriteBytes(payload)
	}
}

// BenchmarkFreshBuf_WriteLarge_64KB is the mid-size analogue of the 1MB
// case, matching HTTP body and WebSocket frame sizes.
func BenchmarkFreshBuf_WriteLarge_64KB(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 64*1024)
	b.ReportAllocs()
	for b.Loop() {
		buf := EmptyByteBuf()
		buf.WriteBytes(payload)
	}
}

// BenchmarkReset_ReuseCycle writes and Resets the same buffer to confirm
// that reuse is allocation-free once the backing array has warmed up.
func BenchmarkReset_ReuseCycle(b *testing.B) {
	payload := bytes.Repeat([]byte("y"), 512)
	buf := EmptyByteBuf()
	buf.WriteBytes(payload)
	buf.Reset()
	b.ReportAllocs()
	for b.Loop() {
		buf.WriteBytes(payload)
		buf.Reset()
	}
}

// BenchmarkWriteReader_Reuse drives WriteReader through a reused reader and
// a pre-sized destination to isolate its own allocation cost.
func BenchmarkWriteReader_Reuse(b *testing.B) {
	payload := bytes.Repeat([]byte("r"), 8*1024)
	buf := EmptyByteBuf().(*DefaultByteBuf)
	buf.EnsureCapacity(16 * 1024)
	reader := bytes.NewReader(payload)
	b.ReportAllocs()
	for b.Loop() {
		buf.Reset()
		reader.Reset(payload)
		buf.WriteReader(reader)
	}
}

// BenchmarkSkip_HotLoop measures Skip as pure index arithmetic.
func BenchmarkSkip_HotLoop(b *testing.B) {
	buf := EmptyByteBuf().(*DefaultByteBuf)
	buf.WriteBytes(bytes.Repeat([]byte("z"), 1<<20))
	b.ReportAllocs()
	for b.Loop() {
		buf.readerIndex = 0
		buf.Skip(256)
	}
}

var _ io.Reader = (*bytes.Reader)(nil)
