package buf

import (
	"bytes"
	"io"
	"net"
	"testing"
)

// BenchmarkPool_AcquireRelease measures the per-operation cost of getting a
// buffer from the pool and returning it.
func BenchmarkPool_AcquireRelease(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		buf := AcquireByteBuf(1024)
		ReleaseByteBuf(buf)
	}
}

// BenchmarkPool_vs_EmptyByteBuf contrasts AcquireByteBuf's cost with
// allocating a fresh buffer for the same capacity.
func BenchmarkPool_vs_EmptyByteBuf(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		buf := EmptyByteBuf().(*DefaultByteBuf)
		buf.EnsureCapacity(1024)
	}
}

// BenchmarkComposite_AddReadBytes_ZeroCopyFastPath exercises the common
// single-component case.
func BenchmarkComposite_AddReadBytes_ZeroCopyFastPath(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1024)
	wrap := NewSharedByteBuf(payload)
	b.ReportAllocs()
	for b.Loop() {
		c := NewCompositeByteBuf(wrap)
		_ = c.ReadBytes(512)
	}
}

// BenchmarkComposite_ReadUInt32_CrossBoundary exercises the slow path
// where a 4-byte read must stitch bytes from 4 different components.
func BenchmarkComposite_ReadUInt32_CrossBoundary(b *testing.B) {
	p := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	b0 := NewSharedByteBuf(p[0:1])
	b1 := NewSharedByteBuf(p[1:2])
	b2 := NewSharedByteBuf(p[2:3])
	b3 := NewSharedByteBuf(p[3:4])
	b.ReportAllocs()
	for b.Loop() {
		c := NewCompositeByteBuf(b0, b1, b2, b3)
		_ = c.ReadUInt32()
	}
}

// BenchmarkComposite_Bytes_LazyConsolidate measures the first-call
// cross-component consolidation cost. Size is bounded to avoid blowing
// up memory during -count iterations.
func BenchmarkComposite_Bytes_LazyConsolidate(b *testing.B) {
	// 8 components x 256 bytes = 2KB total, safely bounded.
	frags := make([]ByteBuf, 8)
	for i := range frags {
		frags[i] = NewSharedByteBuf(bytes.Repeat([]byte{byte('A' + i)}, 256))
	}
	b.ReportAllocs()
	for b.Loop() {
		c := NewCompositeByteBuf(frags...)
		_ = c.Bytes()
	}
}

// BenchmarkComposite_MergeVsBytesBuffer compares composing many small
// fragments via CompositeByteBuf + WriteTo against the classic
// merge-into-bytes.Buffer approach, on an in-memory writer.
func BenchmarkComposite_MergeVsBytesBuffer(b *testing.B) {
	frags := [][]byte{
		bytes.Repeat([]byte{'A'}, 16),
		bytes.Repeat([]byte{'B'}, 64),
		bytes.Repeat([]byte{'C'}, 256),
		bytes.Repeat([]byte{'D'}, 1024),
	}
	wrapped := make([]ByteBuf, len(frags))
	for i, f := range frags {
		wrapped[i] = NewSharedByteBuf(f)
	}

	b.Run("composite_writeto", func(b *testing.B) {
		b.ReportAllocs()
		var sink bytes.Buffer
		for b.Loop() {
			sink.Reset()
			c := NewCompositeByteBuf(wrapped...)
			_, _ = c.WriteTo(&sink)
		}
	})

	b.Run("bytes_buffer_join", func(b *testing.B) {
		b.ReportAllocs()
		var sink bytes.Buffer
		for b.Loop() {
			sink.Reset()
			for _, f := range frags {
				sink.Write(f)
			}
		}
	})
}

// BenchmarkComposite_WriteTo_TCPLoopback measures scatter-gather
// performance over a real TCP socket so the OS writev(2) path is
// exercised. Payload is bounded (~5KB per iteration) and the sink is a
// small goroutine that drains; we stop the bench clock before the TCP
// setup and after teardown.
func BenchmarkComposite_WriteTo_TCPLoopback(b *testing.B) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer lis.Close()

	done := make(chan struct{})
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			close(done)
			return
		}
		defer conn.Close()
		// Drain with a small buffer; keep memory footprint tight.
		buf := make([]byte, 8*1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				if err != io.EOF {
					// Connection closed by benchmark teardown is expected.
				}
				close(done)
				return
			}
		}
	}()

	client, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		client.Close()
		<-done
	}()

	frags := []ByteBuf{
		NewSharedByteBuf(bytes.Repeat([]byte{'h'}, 64)),
		NewSharedByteBuf(bytes.Repeat([]byte{'b'}, 512)),
		NewSharedByteBuf(bytes.Repeat([]byte{'t'}, 4096)),
	}

	b.ReportAllocs()
	for b.Loop() {
		c := NewCompositeByteBuf(frags...)
		if _, err := c.WriteTo(client); err != nil {
			b.Fatal(err)
		}
	}
}
