package buf

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultByteBuf_Write(t *testing.T) {
	buf := EmptyByteBuf()
	buf.Write([]byte{1})
	buf.Write([]byte{1, 1, 1})
	buf.Write([]byte{2})
	buf.Write([]byte{3})
	assert.Equal(t, []byte{1, 1, 1, 1, 2, 3}, buf.Bytes())
	readBuf := make([]byte, buf.ReadableBytes())
	n, _ := buf.Read(readBuf)
	assert.Equal(t, len(readBuf), n)
	assert.Equal(t, []byte{1, 1, 1, 1, 2, 3}, readBuf)
	assert.Equal(t, []byte{}, buf.Bytes())
}

func TestDefaultByteBuf_WriteAt(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteAt([]byte{1}, 0)
	buf.WriteAt([]byte{1, 1, 1}, 5)
	buf.WriteAt([]byte{2}, 1)
	buf.WriteAt([]byte{3}, 2)
	assert.Equal(t, []byte{1, 2, 3, 0, 0, 1, 1, 1}, buf.Bytes())
}

func TestDefaultByteBuf_WriteInt16(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteInt16(math.MaxInt16)
	assert.EqualValues(t, math.MaxInt16, buf.ReadInt16())
}

func TestDefaultByteBuf_WriteInt32(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteInt32(math.MaxInt32)
	assert.EqualValues(t, math.MaxInt32, buf.ReadInt32())
}

func TestDefaultByteBuf_WriteInt64(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteInt64(math.MaxInt64)
	assert.EqualValues(t, math.MaxInt64, buf.ReadInt64())
}

func TestDefaultByteBuf_WriteUInt16(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteUInt16(math.MaxUint16)
	assert.EqualValues(t, math.MaxUint16, buf.ReadUInt16())
}

func TestDefaultByteBuf_WriteUInt32(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteUInt32(math.MaxUint32)
	assert.EqualValues(t, math.MaxUint32, buf.ReadUInt32())
}

func TestDefaultByteBuf_WriteUInt64(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteUInt64(math.MaxUint64)
	if math.MaxUint64 != buf.ReadUInt64() {
		t.Fail()
	}
}

func TestDefaultByteBuf_WriteFloat32(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteFloat32(math.MaxFloat32)
	if math.MaxFloat32 != buf.ReadFloat32() {
		t.Fail()
	}

	buf.WriteFloat32LE(math.MaxFloat32)
	if math.MaxFloat32 != buf.ReadFloat32LE() {
		t.Fail()
	}
}

func TestDefaultByteBuf_WriteFloat64(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteFloat64(math.MaxFloat64)
	if math.MaxFloat64 != buf.ReadFloat64() {
		t.Fail()
	}

	buf.WriteFloat64LE(math.MaxFloat64)
	if math.MaxFloat64 != buf.ReadFloat64LE() {
		t.Fail()
	}
}

func TestDefaultByteBuf_Reset(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteUInt64(math.MaxUint64)
	buf.Reset()
	assert.EqualValues(t, 0, buf.ReadableBytes())
}

func TestDefaultByteBuf_Mark(t *testing.T) {
	buf := EmptyByteBuf()
	buf.MarkWriterIndex()
	buf.WriteUInt64(math.MaxInt64)
	buf.MarkReaderIndex()
	assert.EqualValues(t, 8, buf.WriterIndex())
	assert.EqualValues(t, 0, buf.ReaderIndex())
	assert.EqualValues(t, math.MaxInt64, buf.ReadInt64())
	assert.EqualValues(t, 8, buf.ReaderIndex())
	buf.ResetWriterIndex()
	buf.ResetReaderIndex()
	assert.EqualValues(t, 0, buf.WriterIndex())
	assert.EqualValues(t, 0, buf.ReaderIndex())
	buf.WriteUInt64(math.MaxInt64 - 1)
	assert.EqualValues(t, 8, buf.WriterIndex())
	assert.EqualValues(t, math.MaxInt64-1, buf.ReadInt64())
	assert.EqualValues(t, 8, buf.ReaderIndex())
	assert.EqualValues(t, 0, buf.ReadableBytes())
	buf.Reset()
	buf.WriteString("ok")
	assert.EqualValues(t, "ok", string(buf.ReadBytes(buf.ReadableBytes())))
}

func TestDefaultByteBuf_Grow(t *testing.T) {
	buf := EmptyByteBuf()
	buf.AppendByte(0x01)
	assert.EqualValues(t, 32, buf.Cap())
	assert.EqualValues(t, 1, buf.ReadableBytes())
	buf.ReadBytes(1)
	assert.EqualValues(t, 32, buf.Cap())
	assert.EqualValues(t, 0, buf.ReadableBytes())
	buf.WriteString("abcdef")
	buf.ReadBytes(5)
	assert.EqualValues(t, 32, buf.Cap())
	assert.EqualValues(t, 1, buf.ReadableBytes())
	buf.WriteString("abcdeabcdeabcdeabcdeabcdeabcde")
	assert.EqualValues(t, 64, buf.Cap())
	assert.EqualValues(t, 31, buf.ReadableBytes())
}

func TestDefaultByteBuf_Read(t *testing.T) {
	buf := EmptyByteBuf()
	buf.WriteString("0x01")
	assert.EqualValues(t, 32, buf.Cap())
	assert.EqualValues(t, 4, buf.ReadableBytes())
	slice := []byte{}
	read, err := buf.Read(slice)
	assert.EqualValues(t, 0, read)
	assert.Nil(t, err)
	slice = []byte{0}
	read, err = buf.Read(slice)
	assert.EqualValues(t, '0', slice[0])
	assert.EqualValues(t, 1, read)
	assert.Nil(t, err)
	assert.EqualValues(t, 3, buf.ReadableBytes())
}

// slowReader emits data in small chunks to exercise chunked WriteReader
type slowReader struct {
	total int
	chunk int
	pat   byte
	pos   int
}

func (sr *slowReader) Read(p []byte) (int, error) {
	if sr.pos >= sr.total {
		return 0, io.EOF
	}
	n := sr.chunk
	if n > len(p) {
		n = len(p)
	}
	remain := sr.total - sr.pos
	if n > remain {
		n = remain
	}
	for i := 0; i < n; i++ {
		p[i] = byte(int(sr.pat) + ((sr.pos + i) % 251))
	}
	sr.pos += n
	return n, nil
}

// shortWriter simulates a writer that returns partial write and an error
type shortWriter struct {
	max int
	buf bytes.Buffer
}

func (sw *shortWriter) Write(p []byte) (int, error) {
	if sw.max <= 0 {
		return 0, errors.New("no space")
	}
	n := sw.max
	if n > len(p) {
		n = len(p)
	}
	sw.buf.Write(p[:n])
	// return n with an error to simulate short write with error
	return n, errors.New("forced error after partial write")
}

func TestReadBytes_NegativeLen_Panic(t *testing.T) {
	buf := EmptyByteBuf()
	assert.Panics(t, func() { buf.ReadBytes(-1) })
}

func TestWriteAt_InvalidOffset_Panic(t *testing.T) {
	buf := EmptyByteBuf()
	// negative offset
	assert.Panics(t, func() { _, _ = buf.(*DefaultByteBuf).WriteAt([]byte{1}, -1) })

	// extremely large offset leading to overflow when converted to int
	maxInt := int(^uint(0) >> 1)
	offset := int64(maxInt)
	assert.Panics(t, func() { _, _ = buf.(*DefaultByteBuf).WriteAt([]byte{1}, offset) })
}

func TestWriteReader_ChunkedLargeInput(t *testing.T) {
	buf := EmptyByteBuf()
	total := 256 * 1024 // 256KB
	sr := &slowReader{total: total, chunk: 1023, pat: 7}
	buf.WriteReader(sr)
	assert.Equal(t, total, buf.ReadableBytes())
	// quick spot check content pattern
	bs := buf.ReadBytes(10)
	for i := 0; i < 10; i++ {
		assert.Equal(t, byte(7+(i%251)), bs[i])
	}
}

func TestReadWriter_ShortWrite_PanicAndConsume(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("hello")
	before := b.ReadableBytes()
	w := &shortWriter{max: 2}
	assert.Panics(t, func() { b.ReadWriter(w) })
	// 2 bytes should be consumed before panic
	after := b.ReadableBytes()
	assert.Equal(t, before-2, after)
}

func TestRoundTripRandom_BE_LE(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	t.Run("int16", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]int16, 1000)
		for i := range vals {
			vals[i] = int16(r.Int())
			b.WriteInt16(vals[i])
		}
		for i := range vals {
			assert.EqualValues(t, vals[i], b.ReadInt16())
		}
	})

	t.Run("int32", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]int32, 1000)
		for i := range vals {
			vals[i] = int32(r.Int())
			b.WriteInt32(vals[i])
		}
		for i := range vals {
			assert.EqualValues(t, vals[i], b.ReadInt32())
		}
	})

	t.Run("int64", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]int64, 1000)
		for i := range vals {
			vals[i] = r.Int63()
			b.WriteInt64(vals[i])
		}
		for i := range vals {
			assert.EqualValues(t, vals[i], b.ReadInt64())
		}
	})

	t.Run("uint16", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]uint16, 1000)
		for i := range vals {
			vals[i] = uint16(r.Uint32())
			b.WriteUInt16(vals[i])
		}
		for i := range vals {
			assert.EqualValues(t, vals[i], b.ReadUInt16())
		}
	})

	t.Run("uint32", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]uint32, 1000)
		for i := range vals {
			vals[i] = r.Uint32()
			b.WriteUInt32(vals[i])
		}
		for i := range vals {
			assert.EqualValues(t, vals[i], b.ReadUInt32())
		}
	})

	t.Run("uint64", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]uint64, 1000)
		for i := range vals {
			vals[i] = r.Uint64()
			b.WriteUInt64(vals[i])
		}
		for i := range vals {
			assert.EqualValues(t, vals[i], b.ReadUInt64())
		}
	})

	t.Run("float32-BE-LE", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]float32, 1000)
		for i := range vals {
			vals[i] = r.Float32()*2 - 1 // [-1,1]
			b.WriteFloat32(vals[i])
		}
		for i := range vals {
			if math.IsNaN(float64(vals[i])) {
				assert.True(t, math.IsNaN(float64(b.ReadFloat32())))
			} else {
				assert.InDelta(t, vals[i], b.ReadFloat32(), 0)
			}
		}

		b = EmptyByteBuf()
		for i := range vals {
			b.WriteFloat32LE(vals[i])
		}
		for i := range vals {
			if math.IsNaN(float64(vals[i])) {
				assert.True(t, math.IsNaN(float64(b.ReadFloat32LE())))
			} else {
				assert.InDelta(t, vals[i], b.ReadFloat32LE(), 0)
			}
		}
	})

	t.Run("float64-BE-LE", func(t *testing.T) {
		b := EmptyByteBuf()
		vals := make([]float64, 1000)
		for i := range vals {
			vals[i] = r.NormFloat64() // normal distribution
			b.WriteFloat64(vals[i])
		}
		for i := range vals {
			if math.IsNaN(vals[i]) {
				assert.True(t, math.IsNaN(b.ReadFloat64()))
			} else {
				assert.InDelta(t, vals[i], b.ReadFloat64(), 0)
			}
		}

		b = EmptyByteBuf()
		for i := range vals {
			b.WriteFloat64LE(vals[i])
		}
		for i := range vals {
			if math.IsNaN(vals[i]) {
				assert.True(t, math.IsNaN(b.ReadFloat64LE()))
			} else {
				assert.InDelta(t, vals[i], b.ReadFloat64LE(), 0)
			}
		}
	})
}

func TestWriteString_Large(t *testing.T) {
	b := EmptyByteBuf()
	s := bytes.Repeat([]byte("abc"), 10000) // 30KB
	b.WriteString(string(s))
	assert.Equal(t, len(s), b.ReadableBytes())
	got := b.ReadBytes(len(s))
	assert.Equal(t, s, got)
}

func TestGrowAndMark_WithExpansion(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("abcdef")
	b.MarkReaderIndex() // mark at 0
	_ = b.ReadBytes(5)
	// Force grow
	big := bytes.Repeat([]byte{'x'}, 100000)
	b.WriteBytes(big)
	// Reset to mark should still be valid (0)
	b.ResetReaderIndex()
	assert.Equal(t, 0, b.ReaderIndex())
}

func TestBytesView_MutationAffectsBuffer_DocumentedBehavior(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("hello")
	view := b.Bytes()
	view[0] = 'H'
	assert.Equal(t, "Hello", string(b.Bytes()))
}

func TestBytesCopy_ReturnsIndependentSlice(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("world")
	cp := b.BytesCopy()
	assert.Equal(t, []byte("world"), cp)
	// mutate copy should not affect original
	cp[0] = 'W'
	assert.Equal(t, "world", string(b.Bytes()))
}

func init() {
	// Stabilize any time-based randomness if used
	rand.Seed(time.Now().UnixNano())
}

// Additional comprehensive test cases for better coverage

func TestClose_ResetsAllIndices(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("test data")
	b.MarkReaderIndex()
	b.MarkWriterIndex()
	_ = b.ReadBytes(4)

	err := b.Close()
	assert.NoError(t, err)
	assert.Equal(t, 0, b.ReaderIndex())
	assert.Equal(t, 0, b.WriterIndex())
	assert.Equal(t, 0, b.ReadableBytes())
	assert.Equal(t, 0, b.Cap())
}

func TestCompact_EdgeCases(t *testing.T) {
	t.Run("already_at_start", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("hello")
		// readerIndex is already 0
		result := b.(*DefaultByteBuf).Compact()
		assert.Same(t, b, result) // should return self
		assert.Equal(t, "hello", string(b.Bytes()))
	})

	t.Run("empty_readable", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("test")
		_ = b.ReadBytes(4) // consume all
		b.(*DefaultByteBuf).Compact()
		assert.Equal(t, 0, b.ReaderIndex())
		assert.Equal(t, 0, b.WriterIndex())
		assert.Equal(t, 0, b.ReadableBytes())
	})

	t.Run("with_marked_indices", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("abcdef")
		b.MarkReaderIndex() // mark at 0
		b.MarkWriterIndex() // mark at 6
		_ = b.ReadBytes(3)  // readerIndex=3
		b.(*DefaultByteBuf).Compact()

		assert.Equal(t, 0, b.ReaderIndex())
		assert.Equal(t, 3, b.WriterIndex())
		assert.Equal(t, "def", string(b.Bytes()))
		// marked indices should be adjusted
		assert.Equal(t, 0, b.(*DefaultByteBuf).prevReaderIndex)
		assert.Equal(t, 3, b.(*DefaultByteBuf).prevWriterIndex)
	})
}

func TestEnsureCapacity_EdgeCases(t *testing.T) {
	t.Run("negative_capacity", func(t *testing.T) {
		b := EmptyByteBuf()
		assert.Panics(t, func() { b.(*DefaultByteBuf).EnsureCapacity(-1) })
	})

	t.Run("zero_capacity", func(t *testing.T) {
		b := EmptyByteBuf()
		result := b.(*DefaultByteBuf).EnsureCapacity(0)
		assert.Same(t, b, result)
	})

	t.Run("already_sufficient", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("test") // cap=32, used=4, available=28
		result := b.(*DefaultByteBuf).EnsureCapacity(20)
		assert.Same(t, b, result)
		assert.Equal(t, 32, b.Cap()) // should not grow
	})

	t.Run("compact_sufficient", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("hello world test data") // ensure some capacity
		_ = b.ReadBytes(10)                    // create gap at start, readerIndex=10
		oldCap := b.Cap()
		readable := b.ReadableBytes() // remaining bytes

		// request space that needs compaction but fits after compaction
		spaceNeeded := oldCap - readable - 5 // ensure we need compaction but can fit
		b.(*DefaultByteBuf).EnsureCapacity(spaceNeeded)
		assert.Equal(t, oldCap, b.Cap())    // should not grow
		assert.Equal(t, 0, b.ReaderIndex()) // should be compacted
	})

	t.Run("needs_growth", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("test") // cap=32
		oldCap := b.Cap()
		b.(*DefaultByteBuf).EnsureCapacity(100) // larger than current cap
		assert.Greater(t, b.Cap(), oldCap)
		assert.GreaterOrEqual(t, b.Cap(), b.ReadableBytes()+100)
	})
}

func TestMarkReset_ComplexScenarios(t *testing.T) {
	t.Run("nested_marks", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("abcdefghij")

		// First mark
		b.MarkReaderIndex() // mark at 0
		_ = b.ReadBytes(3)  // read "abc", readerIndex=3

		// Second mark (overrides first)
		b.MarkReaderIndex() // mark at 3
		_ = b.ReadBytes(2)  // read "de", readerIndex=5

		// Reset should go to second mark (3)
		b.ResetReaderIndex()
		assert.Equal(t, 3, b.ReaderIndex())
		assert.Equal(t, "defghij", string(b.Bytes()))
	})

	t.Run("writer_mark_reset", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("hello")
		b.MarkWriterIndex() // mark at 5
		b.WriteString(" world")
		assert.Equal(t, 11, b.WriterIndex())

		b.ResetWriterIndex()
		assert.Equal(t, 5, b.WriterIndex())
		assert.Equal(t, "hello", string(b.Bytes()))
	})

	t.Run("mark_after_grow", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("small")
		b.MarkReaderIndex()
		b.MarkWriterIndex()

		// Force growth
		big := bytes.Repeat([]byte("x"), 1000)
		b.WriteBytes(big)

		// Marks should still be valid
		b.ResetReaderIndex()
		b.ResetWriterIndex()
		assert.Equal(t, 0, b.ReaderIndex())
		assert.Equal(t, 5, b.WriterIndex())
		assert.Equal(t, "small", string(b.Bytes()))
	})
}

func TestPanicConditions(t *testing.T) {
	t.Run("read_byte_empty", func(t *testing.T) {
		b := EmptyByteBuf()
		assert.Panics(t, func() { b.MustReadByte() })
	})

	t.Run("read_bytes_insufficient", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("hi")
		assert.Panics(t, func() { b.ReadBytes(5) })
	})

	t.Run("write_bytebuf_nil", func(t *testing.T) {
		b := EmptyByteBuf()
		assert.Panics(t, func() { b.WriteByteBuf(nil) })
	})

	t.Run("write_reader_nil", func(t *testing.T) {
		b := EmptyByteBuf()
		assert.Panics(t, func() { b.WriteReader(nil) })
	})

	t.Run("write_at_overflow", func(t *testing.T) {
		b := EmptyByteBuf()
		maxInt := int(^uint(0) >> 1)
		assert.Panics(t, func() {
			_, _ = b.(*DefaultByteBuf).WriteAt([]byte("test"), int64(maxInt-1))
		})
	})
}

func TestSkip_Various(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("hello world")

	b.Skip(5) // skip "hello"
	assert.Equal(t, " world", string(b.Bytes()))

	b.Skip(1) // skip " "
	assert.Equal(t, "world", string(b.Bytes()))

	// Skip more than available should panic
	assert.Panics(t, func() { b.Skip(10) })
}

func TestClone_IndependentCopy(t *testing.T) {
	original := EmptyByteBuf()
	original.WriteString("original data")
	original.MarkReaderIndex()
	_ = original.ReadBytes(8) // read "original", leaving " data"

	clone := original.Clone()

	// Clone should have same readable content
	assert.Equal(t, " data", string(clone.Bytes()))

	// But should be independent - clone returns only the readable portion
	// so clone starts fresh without sharing the same underlying buffer
	clone.WriteString(" modified")
	assert.Equal(t, " data", string(original.Bytes()))
	assert.Equal(t, " data modified", string(clone.Bytes()))

	// Original marks should not affect clone since clone is independent
	original.ResetReaderIndex()
	assert.Equal(t, " data modified", string(clone.Bytes())) // clone is independent
}

func TestWriteAt_ExtendsBeyondCurrent(t *testing.T) {
	b := EmptyByteBuf()

	// Write beyond current buffer
	n, err := b.(*DefaultByteBuf).WriteAt([]byte("hello"), 10)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, 15, b.WriterIndex()) // should extend
	assert.Equal(t, 15, len(b.Bytes()))

	// Check content
	bytes := b.Bytes()
	assert.Equal(t, make([]byte, 10), bytes[:10]) // zeros
	assert.Equal(t, []byte("hello"), bytes[10:15])
}

func TestWriteReader_ErrorHandling(t *testing.T) {
	t.Run("reader_returns_error", func(t *testing.T) {
		b := EmptyByteBuf()
		errorReader := &errorReaderType{data: []byte("test"), errAfter: 2}

		assert.Panics(t, func() { b.WriteReader(errorReader) })
		// Should have written all data before the error occurs
		assert.Equal(t, "test", string(b.Bytes()))
	})
}

// errorReaderType for testing error handling
type errorReaderType struct {
	data     []byte
	pos      int
	errAfter int
}

func (er *errorReaderType) Read(p []byte) (int, error) {
	if er.pos >= er.errAfter {
		return 0, errors.New("test error")
	}
	if er.pos >= len(er.data) {
		return 0, io.EOF
	}

	n := copy(p, er.data[er.pos:])
	er.pos += n
	return n, nil
}

func TestLittleEndian_AllTypes(t *testing.T) {
	b := EmptyByteBuf()

	// Write all LE types
	b.WriteInt16LE(-12345)
	b.WriteInt32LE(-1234567890)
	b.WriteInt64LE(-1234567890123456789)
	b.WriteUInt16LE(54321)
	b.WriteUInt32LE(3234567890)
	b.WriteUInt64LE(12345678901234567890)
	b.WriteFloat32LE(3.14159)
	b.WriteFloat64LE(2.718281828459045)

	// Read back and verify
	assert.Equal(t, int16(-12345), b.ReadInt16LE())
	assert.Equal(t, int32(-1234567890), b.ReadInt32LE())
	assert.Equal(t, int64(-1234567890123456789), b.ReadInt64LE())
	assert.Equal(t, uint16(54321), b.ReadUInt16LE())
	assert.Equal(t, uint32(3234567890), b.ReadUInt32LE())
	assert.Equal(t, uint64(12345678901234567890), b.ReadUInt64LE())
	assert.InDelta(t, float32(3.14159), b.ReadFloat32LE(), 0.0001)
	assert.InDelta(t, 2.718281828459045, b.ReadFloat64LE(), 0.0000001)
}

func TestRead_IOInterface(t *testing.T) {
	t.Run("partial_read", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("hello world")

		small := make([]byte, 5)
		n, err := b.Read(small)
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(small))
		assert.Equal(t, " world", string(b.Bytes()))
	})

	t.Run("read_empty_returns_eof", func(t *testing.T) {
		b := EmptyByteBuf()
		buf := make([]byte, 10)
		n, err := b.Read(buf)
		assert.Equal(t, io.EOF, err)
		assert.Equal(t, 0, n)
	})

	t.Run("read_zero_bytes", func(t *testing.T) {
		b := EmptyByteBuf()
		b.WriteString("test")
		var empty []byte
		n, err := b.Read(empty)
		assert.NoError(t, err)
		assert.Equal(t, 0, n)
		assert.Equal(t, "test", string(b.Bytes())) // unchanged
	})
}

func TestWrite_IOInterface(t *testing.T) {
	t.Run("write_empty_slice", func(t *testing.T) {
		b := EmptyByteBuf()
		n, err := b.Write([]byte{})
		assert.NoError(t, err)
		assert.Equal(t, 0, n)
		assert.Equal(t, 0, b.ReadableBytes())
	})

	t.Run("write_returns_correct_count", func(t *testing.T) {
		b := EmptyByteBuf()
		data := []byte("test data")
		n, err := b.Write(data)
		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, "test data", string(b.Bytes()))
	})
}

func TestCompact_MovesReadableToStart(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("abcdef")
	// consume 2 bytes so readerIndex > 0
	_ = b.ReadBytes(2)
	// mark indices to verify they get adjusted reasonably
	b.MarkReaderIndex()
	b.MarkWriterIndex()
	// compact should move remaining "cdef" to start
	b.(*DefaultByteBuf).Compact()
	assert.Equal(t, 0, b.ReaderIndex())
	assert.Equal(t, 4, b.ReadableBytes())
	assert.Equal(t, []byte("cdef"), b.Bytes())
}

func TestEnsureCapacity_CompactThenGrow(t *testing.T) {
	b := EmptyByteBuf()
	b.WriteString("abcdef") // cap=32, len=6
	_ = b.ReadBytes(5)      // readerIndex=5, readable=1
	// currently writerIndex=6, readerIndex=5, cap=32, writable=26
	// EnsureCapacity larger than remaining without compaction would pass, so we request big number
	b.(*DefaultByteBuf).EnsureCapacity(40) // require > cap to trigger grow
	// after EnsureCapacity, capacity must be >= readable+40
	assert.GreaterOrEqual(t, b.Cap(), b.ReadableBytes()+40)
	// and data should be compacted at start
	assert.Equal(t, 0, b.ReaderIndex())
	assert.Equal(t, []byte("f"), b.Bytes())
}

// Test io.ByteWriter and io.ByteReader interface compatibility
func TestStandardInterfaceCompatibility(t *testing.T) {
	t.Run("io.ByteWriter_interface", func(t *testing.T) {
		buf := EmptyByteBuf()
		var writer io.ByteWriter = buf

		err := writer.WriteByte('A')
		assert.NoError(t, err)
		assert.Equal(t, 1, buf.ReadableBytes())

		data := buf.ReadBytes(1)
		assert.Equal(t, []byte{'A'}, data)
	})

	t.Run("io.ByteReader_interface", func(t *testing.T) {
		buf := EmptyByteBuf()
		buf.WriteString("Hello")

		var reader io.ByteReader = buf

		// Read each byte with error handling
		b1, err := reader.ReadByte()
		assert.NoError(t, err)
		assert.Equal(t, byte('H'), b1)

		b2, err := reader.ReadByte()
		assert.NoError(t, err)
		assert.Equal(t, byte('e'), b2)

		// Continue reading all bytes
		for i := 0; i < 3; i++ {
			_, err := reader.ReadByte()
			assert.NoError(t, err)
		}

		// Try reading from empty buffer
		_, err = reader.ReadByte()
		assert.Error(t, err)
		assert.Equal(t, ErrInsufficientSize, err)
	})

	t.Run("combined_interface_usage", func(t *testing.T) {
		buf := EmptyByteBuf()

		// Use as io.ByteWriter
		var writer io.ByteWriter = buf
		for i := 0; i < 10; i++ {
			err := writer.WriteByte(byte('0' + i))
			assert.NoError(t, err)
		}

		// Use as io.ByteReader
		var reader io.ByteReader = buf
		for i := 0; i < 10; i++ {
			b, err := reader.ReadByte()
			assert.NoError(t, err)
			assert.Equal(t, byte('0'+i), b)
		}

		// Should be empty now
		_, err := reader.ReadByte()
		assert.Error(t, err)
	})
}
