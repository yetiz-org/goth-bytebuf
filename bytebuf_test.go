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
	buf.WriteByte(0x01)
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
