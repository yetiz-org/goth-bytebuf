package buf

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
