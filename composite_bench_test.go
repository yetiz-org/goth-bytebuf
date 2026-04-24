package buf

import (
	"bytes"
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
// case: one component, readable region fits inside it, ReadBytes aliases
// the component without copying.
func BenchmarkComposite_AddReadBytes_ZeroCopyFastPath(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1024)
	b.ReportAllocs()
	for b.Loop() {
		c := NewCompositeByteBuf(payload)
		_ = c.ReadBytes(512)
	}
}

// BenchmarkComposite_MergeVsBytesBuffer compares composing many small
// fragments via CompositeByteBuf + WriteTo against the classic merge-into-
// bytes.Buffer approach, on an in-memory writer.
func BenchmarkComposite_MergeVsBytesBuffer(b *testing.B) {
	frags := [][]byte{
		bytes.Repeat([]byte{'A'}, 16),
		bytes.Repeat([]byte{'B'}, 64),
		bytes.Repeat([]byte{'C'}, 256),
		bytes.Repeat([]byte{'D'}, 1024),
	}

	b.Run("composite_writeto", func(b *testing.B) {
		b.ReportAllocs()
		var sink bytes.Buffer
		for b.Loop() {
			sink.Reset()
			c := NewCompositeByteBuf(frags...)
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
// performance over a real TCP socket so the OS writev(2) path is exercised.
func BenchmarkComposite_WriteTo_TCPLoopback(b *testing.B) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer lis.Close()

	sink := make(chan int, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64*1024)
		total := 0
		for {
			n, err := conn.Read(buf)
			total += n
			if err != nil {
				sink <- total
				return
			}
		}
	}()

	client, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	frags := [][]byte{
		bytes.Repeat([]byte{'h'}, 64),
		bytes.Repeat([]byte{'b'}, 512),
		bytes.Repeat([]byte{'t'}, 4096),
	}

	b.ReportAllocs()
	for b.Loop() {
		c := NewCompositeByteBuf(frags...)
		_, err := c.WriteTo(client)
		if err != nil {
			b.Fatal(err)
		}
	}
}
