package buf

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unsafe"
)

type ByteBuf interface {
	io.Writer
	io.Reader
	io.Closer
	io.WriterAt
	ReaderIndex() int
	WriterIndex() int
	MarkReaderIndex() ByteBuf
	ResetReaderIndex() ByteBuf
	MarkWriterIndex() ByteBuf
	ResetWriterIndex() ByteBuf
	Reset() ByteBuf
	Bytes() []byte
	ReadableBytes() int
	Cap() int
	Grow(v int) ByteBuf
	Skip(v int) ByteBuf
	Clone() ByteBuf
	WriteByte(c byte) ByteBuf
	WriteBytes(bs []byte) ByteBuf
	WriteString(s string) ByteBuf
	WriteByteBuf(buf ByteBuf) ByteBuf
	WriteReader(reader io.Reader) ByteBuf
	WriteInt16(v int16) ByteBuf
	WriteInt32(v int32) ByteBuf
	WriteInt64(v int64) ByteBuf
	WriteUInt16(v uint16) ByteBuf
	WriteUInt32(v uint32) ByteBuf
	WriteUInt64(v uint64) ByteBuf
	WriteFloat32(v float32) ByteBuf
	WriteFloat64(v float64) ByteBuf
	WriteInt16LE(v int16) ByteBuf
	WriteInt32LE(v int32) ByteBuf
	WriteInt64LE(v int64) ByteBuf
	WriteUInt16LE(v uint16) ByteBuf
	WriteUInt32LE(v uint32) ByteBuf
	WriteUInt64LE(v uint64) ByteBuf
	WriteFloat32LE(v float32) ByteBuf
	WriteFloat64LE(v float64) ByteBuf
	ReadByte() byte
	ReadBytes(len int) []byte
	ReadByteBuf(len int) ByteBuf
	ReadWriter(writer io.Writer) ByteBuf
	ReadInt16() int16
	ReadInt32() int32
	ReadInt64() int64
	ReadUInt16() uint16
	ReadUInt32() uint32
	ReadUInt64() uint64
	ReadFloat32() float32
	ReadFloat64() float64
	ReadInt16LE() int16
	ReadInt32LE() int32
	ReadInt64LE() int64
	ReadUInt16LE() uint16
	ReadUInt32LE() uint32
	ReadUInt64LE() uint64
	ReadFloat32LE() float32
	ReadFloat64LE() float64
}

var ErrNilObject = fmt.Errorf("nil object")
var ErrInsufficientSize = fmt.Errorf("insufficient size")

func NewByteBuf(bs []byte) ByteBuf {
	buf := &DefaultByteBuf{}
	buf.WriteBytes(bs)
	return buf
}

func NewByteBufString(str string) ByteBuf {
	buf := &DefaultByteBuf{}
	buf.WriteString(str)
	return buf
}

func EmptyByteBuf() ByteBuf {
	return &DefaultByteBuf{}
}

type DefaultByteBuf struct {
	buf                                                        []byte
	readerIndex, writerIndex, prevReaderIndex, prevWriterIndex int
}

func (b *DefaultByteBuf) Write(p []byte) (n int, err error) {
	pl := len(p)
	if pl == 0 {
		return 0, nil
	}

	b.prepare(pl)
	copy(b.buf[b.writerIndex:], p)
	b.writerIndex += pl
	return pl, nil
}

func (b *DefaultByteBuf) Read(p []byte) (n int, err error) {
	cpLen := b.ReadableBytes()
	if cpLen == 0 {
		return 0, io.EOF
	}

	if cpLen > len(p) {
		cpLen = len(p)
	}

	copy(p, b.buf[b.readerIndex:b.readerIndex+cpLen])
	b.readerIndex += cpLen
	return cpLen, nil
}

func (b *DefaultByteBuf) WriteAt(p []byte, offset int64) (n int, err error) {
	pl := len(p)
	if pl == 0 {
		return 0, nil
	}

	expLen := int(offset) + pl
	if expLen > b.Cap() {
		b.prepare(expLen - b.Cap())
	}
	if expLen > b.writerIndex {
		b.writerIndex = expLen
	}

	copy(b.buf[offset:], p)
	return pl, nil
}

func (b *DefaultByteBuf) Close() error {
	b.Reset()
	return nil
}

func (b *DefaultByteBuf) ReaderIndex() int {
	return b.readerIndex
}

func (b *DefaultByteBuf) WriterIndex() int {
	return b.writerIndex
}

func (b *DefaultByteBuf) MarkReaderIndex() ByteBuf {
	b.prevReaderIndex = b.readerIndex
	return b
}

func (b *DefaultByteBuf) ResetReaderIndex() ByteBuf {
	b.readerIndex = b.prevReaderIndex
	b.prevReaderIndex = 0
	return b
}

func (b *DefaultByteBuf) MarkWriterIndex() ByteBuf {
	b.prevWriterIndex = b.writerIndex
	return b
}

func (b *DefaultByteBuf) ResetWriterIndex() ByteBuf {
	b.writerIndex = b.prevWriterIndex
	b.prevWriterIndex = 0
	return b
}

func (b *DefaultByteBuf) Reset() ByteBuf {
	b.buf = []byte{}
	b.readerIndex = 0
	b.writerIndex = 0
	b.prevReaderIndex = 0
	b.prevWriterIndex = 0
	return b
}

func (b *DefaultByteBuf) Bytes() []byte {
	return b.buf[b.readerIndex:b.writerIndex]
}

func (b *DefaultByteBuf) ReadableBytes() int {
	return b.writerIndex - b.readerIndex
}

func (b *DefaultByteBuf) Cap() int {
	return len(b.buf)
}

func (b *DefaultByteBuf) Grow(v int) ByteBuf {
	tb := make([]byte, b.Cap()+v)
	var offset int
	if b.prevReaderIndex == 0 {
		offset = b.readerIndex
	} else {
		offset = b.prevReaderIndex
		b.prevReaderIndex = 0
	}
	copy(tb, b.buf[offset:])
	b.readerIndex -= offset
	b.writerIndex -= offset
	if b.prevWriterIndex > 0 {
		b.prevWriterIndex -= offset
	}

	b.buf = tb
	return b
}

func (b *DefaultByteBuf) Skip(v int) ByteBuf {
	b.ReadBytes(v)
	return b
}

func (b *DefaultByteBuf) Clone() ByteBuf {
	return NewByteBuf(b.Bytes())
}

func (b *DefaultByteBuf) WriteByte(c byte) ByteBuf {
	b.prepare(1)
	b.buf[b.writerIndex] = c
	b.writerIndex++
	return b
}

func (b *DefaultByteBuf) WriteBytes(bs []byte) ByteBuf {
	pl := len(bs)
	b.prepare(pl)
	copy(b.buf[b.writerIndex:], bs)
	b.writerIndex += pl
	return b
}

func (b *DefaultByteBuf) WriteByteBuf(buf ByteBuf) ByteBuf {
	if buf == nil {
		panic(ErrNilObject)
	}

	b.WriteBytes(buf.Bytes())
	return b
}

func (b *DefaultByteBuf) WriteReader(reader io.Reader) ByteBuf {
	if reader == nil {
		panic(ErrNilObject)
	}

	if bs, err := io.ReadAll(reader); err != nil {
		panic(err)
	} else {
		b.WriteBytes(bs)
	}

	return b
}

func (b *DefaultByteBuf) WriteString(s string) ByteBuf {
	b.WriteBytes(*(*[]byte)(unsafe.Pointer(&s)))
	return b
}

func (b *DefaultByteBuf) WriteInt16(v int16) ByteBuf {
	b.WriteUInt16(uint16(v))
	return b
}

func (b *DefaultByteBuf) WriteInt32(v int32) ByteBuf {
	b.WriteUInt32(uint32(v))
	return b
}

func (b *DefaultByteBuf) WriteInt64(v int64) ByteBuf {
	b.WriteUInt64(uint64(v))
	return b
}

func (b *DefaultByteBuf) WriteUInt16(v uint16) ByteBuf {
	bs := make([]byte, 2)
	binary.BigEndian.PutUint16(bs, v)
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteUInt32(v uint32) ByteBuf {
	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, v)
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteUInt64(v uint64) ByteBuf {
	bs := make([]byte, 8)
	binary.BigEndian.PutUint64(bs, v)
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteFloat32(v float32) ByteBuf {
	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, math.Float32bits(v))
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteFloat64(v float64) ByteBuf {
	bs := make([]byte, 8)
	binary.BigEndian.PutUint64(bs, math.Float64bits(v))
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteInt16LE(v int16) ByteBuf {
	b.WriteUInt16LE(uint16(v))
	return b
}

func (b *DefaultByteBuf) WriteInt32LE(v int32) ByteBuf {
	b.WriteUInt32LE(uint32(v))
	return b
}

func (b *DefaultByteBuf) WriteInt64LE(v int64) ByteBuf {
	b.WriteUInt64LE(uint64(v))
	return b
}

func (b *DefaultByteBuf) WriteUInt16LE(v uint16) ByteBuf {
	bs := make([]byte, 2)
	binary.LittleEndian.PutUint16(bs, v)
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteUInt32LE(v uint32) ByteBuf {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, v)
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteUInt64LE(v uint64) ByteBuf {
	bs := make([]byte, 8)
	binary.LittleEndian.PutUint64(bs, v)
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteFloat32LE(v float32) ByteBuf {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, math.Float32bits(v))
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) WriteFloat64LE(v float64) ByteBuf {
	bs := make([]byte, 8)
	binary.LittleEndian.PutUint64(bs, math.Float64bits(v))
	b.prepare(len(bs))
	b.WriteBytes(bs)
	return b
}

func (b *DefaultByteBuf) ReadByte() byte {
	if b.readerIndex == b.writerIndex {
		panic(ErrInsufficientSize)
	}

	b.readerIndex++
	return b.buf[b.readerIndex-1]
}

func (b *DefaultByteBuf) ReadBytes(len int) []byte {
	if len <= 0 {
		return []byte{}
	}

	if b.ReadableBytes() < len {
		panic(ErrInsufficientSize)
	}

	b.readerIndex += len
	return b.buf[b.readerIndex-len : b.readerIndex]
}

func (b *DefaultByteBuf) ReadByteBuf(len int) ByteBuf {
	buf := &DefaultByteBuf{}
	buf.WriteBytes(b.ReadBytes(len))
	return buf
}

func (b *DefaultByteBuf) ReadWriter(writer io.Writer) ByteBuf {
	bs := b.Bytes()
	n, err := writer.Write(bs)
	b.ReadBytes(n)
	if err != nil {
		panic(ErrInsufficientSize)
	}

	return b
}

func (b *DefaultByteBuf) ReadInt16() int16 {
	return int16(b.ReadUInt16())
}

func (b *DefaultByteBuf) ReadInt32() int32 {
	return int32(b.ReadUInt32())
}

func (b *DefaultByteBuf) ReadInt64() int64 {
	return int64(b.ReadUInt64())
}

func (b *DefaultByteBuf) ReadUInt16() uint16 {
	return binary.BigEndian.Uint16(b.ReadBytes(2))
}

func (b *DefaultByteBuf) ReadUInt32() uint32 {
	return binary.BigEndian.Uint32(b.ReadBytes(4))
}

func (b *DefaultByteBuf) ReadUInt64() uint64 {
	return binary.BigEndian.Uint64(b.ReadBytes(8))
}

func (b *DefaultByteBuf) ReadFloat32() float32 {
	return math.Float32frombits(binary.BigEndian.Uint32(b.ReadBytes(4)))
}

func (b *DefaultByteBuf) ReadFloat64() float64 {
	return math.Float64frombits(binary.BigEndian.Uint64(b.ReadBytes(8)))
}

func (b *DefaultByteBuf) ReadInt16LE() int16 {
	return int16(b.ReadUInt16LE())
}

func (b *DefaultByteBuf) ReadInt32LE() int32 {
	return int32(b.ReadUInt32LE())
}

func (b *DefaultByteBuf) ReadInt64LE() int64 {
	return int64(b.ReadUInt64LE())
}

func (b *DefaultByteBuf) ReadUInt16LE() uint16 {
	return binary.LittleEndian.Uint16(b.ReadBytes(2))
}

func (b *DefaultByteBuf) ReadUInt32LE() uint32 {
	return binary.LittleEndian.Uint32(b.ReadBytes(4))
}

func (b *DefaultByteBuf) ReadUInt64LE() uint64 {
	return binary.LittleEndian.Uint64(b.ReadBytes(8))
}

func (b *DefaultByteBuf) ReadFloat32LE() float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b.ReadBytes(4)))
}

func (b *DefaultByteBuf) ReadFloat64LE() float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(b.ReadBytes(8)))
}

func (b *DefaultByteBuf) prepare(i int) {
	if b.Cap() == 0 {
		b.Grow(32)
	}

	for b.writerIndex+i > b.Cap() {
		b.Grow(b.Cap())
	}
}
