package buf

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// Benchmark Write Operations
func BenchmarkAppendByte(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.AppendByte(byte(i % 256))
	}
}

// Benchmark for standard io.ByteWriter interface
func BenchmarkWriteByte(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteByte(byte(i % 256))
	}
}

func BenchmarkWriteBytes_Small(b *testing.B) {
	buf := EmptyByteBuf()
	data := []byte("hello")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkWriteBytes_Medium(b *testing.B) {
	buf := EmptyByteBuf()
	data := bytes.Repeat([]byte("x"), 1024) // 1KB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkWriteBytes_Large(b *testing.B) {
	buf := EmptyByteBuf()
	data := bytes.Repeat([]byte("x"), 64*1024) // 64KB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkWriteString_Small(b *testing.B) {
	buf := EmptyByteBuf()
	s := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteString(s)
	}
}

func BenchmarkWriteString_Large(b *testing.B) {
	buf := EmptyByteBuf()
	s := strings.Repeat("x", 64*1024) // 64KB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteString(s)
	}
}

// Binary Write Operations
func BenchmarkWriteUInt16(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteUInt16(uint16(i))
	}
}

func BenchmarkWriteUInt32(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteUInt32(uint32(i))
	}
}

func BenchmarkWriteUInt64(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteUInt64(uint64(i))
	}
}

func BenchmarkWriteFloat32(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteFloat32(float32(i) * 3.14159)
	}
}

func BenchmarkWriteFloat64(b *testing.B) {
	buf := EmptyByteBuf()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteFloat64(float64(i) * 3.14159)
	}
}

// Benchmark Read Operations
func BenchmarkMustReadByte(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	data := make([]byte, b.N)
	for i := range data {
		data[i] = byte(i % 256)
	}
	buf.WriteBytes(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.MustReadByte()
	}
}

// Benchmark for standard io.ByteReader interface
func BenchmarkReadByte(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	data := make([]byte, b.N)
	for i := range data {
		data[i] = byte(i % 256)
	}
	buf.WriteBytes(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadByte()
	}
}

func BenchmarkReadBytes_Small(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	totalData := bytes.Repeat([]byte("hello"), b.N)
	buf.WriteBytes(totalData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadBytes(5)
	}
}

func BenchmarkReadBytes_Medium(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	chunkSize := 1024
	totalData := bytes.Repeat([]byte("x"), b.N*chunkSize)
	buf.WriteBytes(totalData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadBytes(chunkSize)
	}
}

// Binary Read Operations
func BenchmarkReadUInt16(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	for i := 0; i < b.N; i++ {
		buf.WriteUInt16(uint16(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadUInt16()
	}
}

func BenchmarkReadUInt32(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	for i := 0; i < b.N; i++ {
		buf.WriteUInt32(uint32(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadUInt32()
	}
}

func BenchmarkReadUInt64(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	for i := 0; i < b.N; i++ {
		buf.WriteUInt64(uint64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadUInt64()
	}
}

func BenchmarkReadFloat32(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	for i := 0; i < b.N; i++ {
		buf.WriteFloat32(float32(i) * 3.14159)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadFloat32()
	}
}

func BenchmarkReadFloat64(b *testing.B) {
	buf := EmptyByteBuf()
	// Pre-populate buffer
	for i := 0; i < b.N; i++ {
		buf.WriteFloat64(float64(i) * 3.14159)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadFloat64()
	}
}

// Buffer Management Operations
func BenchmarkCompact(b *testing.B) {
	buf := EmptyByteBuf()
	data := bytes.Repeat([]byte("x"), 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		buf.WriteBytes(data)
		buf.ReadBytes(512) // create gap
		b.StartTimer()

		buf.(*DefaultByteBuf).Compact()
	}
}

func BenchmarkEnsureCapacity(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := EmptyByteBuf()
		buf.(*DefaultByteBuf).EnsureCapacity(1024)
	}
}

func BenchmarkGrow(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := EmptyByteBuf()
		buf.Grow(1024)
	}
}

func BenchmarkClone(b *testing.B) {
	buf := EmptyByteBuf()
	buf.WriteBytes(bytes.Repeat([]byte("x"), 1024))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Clone()
	}
}

// Large Data Operations (>1MB)
func BenchmarkWriteBytes_1MB(b *testing.B) {
	buf := EmptyByteBuf()
	data := bytes.Repeat([]byte("x"), 1024*1024) // 1MB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkWriteBytes_10MB(b *testing.B) {
	buf := EmptyByteBuf()
	data := bytes.Repeat([]byte("x"), 10*1024*1024) // 10MB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkReadBytes_1MB(b *testing.B) {
	buf := EmptyByteBuf()
	chunkSize := 1024 * 1024 // 1MB
	totalData := bytes.Repeat([]byte("x"), b.N*chunkSize)
	buf.WriteBytes(totalData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.ReadBytes(chunkSize)
	}
}

// Memory Allocation Benchmarks
func BenchmarkWriteBytes_Allocs_Small(b *testing.B) {
	buf := EmptyByteBuf()
	data := []byte("hello")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkWriteBytes_Allocs_Medium(b *testing.B) {
	buf := EmptyByteBuf()
	data := bytes.Repeat([]byte("x"), 1024) // 1KB
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteBytes(data)
	}
}

func BenchmarkWriteString_Allocs(b *testing.B) {
	buf := EmptyByteBuf()
	s := strings.Repeat("x", 1024) // 1KB
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteString(s)
	}
}

func BenchmarkGrow_Allocs(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := EmptyByteBuf()
		buf.Grow(1024)
	}
}

// Comparison with bytes.Buffer
func BenchmarkBytesBuffer_Write_Small(b *testing.B) {
	var buf bytes.Buffer
	data := []byte("hello")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(data)
	}
}

func BenchmarkBytesBuffer_Write_Medium(b *testing.B) {
	var buf bytes.Buffer
	data := bytes.Repeat([]byte("x"), 1024) // 1KB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(data)
	}
}

func BenchmarkBytesBuffer_WriteString_Small(b *testing.B) {
	var buf bytes.Buffer
	s := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteString(s)
	}
}

func BenchmarkBytesBuffer_WriteString_Medium(b *testing.B) {
	var buf bytes.Buffer
	s := strings.Repeat("x", 1024) // 1KB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteString(s)
	}
}

func BenchmarkBytesBuffer_Read_Small(b *testing.B) {
	var buf bytes.Buffer
	totalData := bytes.Repeat([]byte("hello"), b.N)
	buf.Write(totalData)

	readBuf := make([]byte, 5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Read(readBuf)
	}
}

// IO Interface Performance
func BenchmarkIOInterface_Write(b *testing.B) {
	var w io.Writer = EmptyByteBuf()
	data := []byte("hello world")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Write(data)
	}
}

func BenchmarkIOInterface_Read(b *testing.B) {
	buf := EmptyByteBuf()
	totalData := bytes.Repeat([]byte("hello world"), b.N)
	buf.WriteBytes(totalData)

	var r io.Reader = buf
	readBuf := make([]byte, 11)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Read(readBuf)
	}
}

// Sequential Operations
func BenchmarkSequentialWriteRead(b *testing.B) {
	data := []byte("test data")
	readBuf := make([]byte, len(data))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := EmptyByteBuf()
		buf.WriteBytes(data)
		buf.Read(readBuf)
	}
}

func BenchmarkSequentialWriteReadUInt32(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := EmptyByteBuf()
		buf.WriteUInt32(uint32(i))
		buf.ReadUInt32()
	}
}

// Index Management
func BenchmarkMarkResetOperations(b *testing.B) {
	buf := EmptyByteBuf()
	buf.WriteBytes(bytes.Repeat([]byte("x"), 1024))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.MarkReaderIndex()
		buf.ReadBytes(10)
		buf.ResetReaderIndex()
	}
}

func BenchmarkWriterMarkReset(b *testing.B) {
	buf := EmptyByteBuf()
	data := []byte("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.MarkWriterIndex()
		buf.WriteBytes(data)
		buf.ResetWriterIndex()
	}
}
