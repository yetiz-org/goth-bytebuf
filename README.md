# GoTH-bytebuf

### make byte stream operation more easily

# HOWTO

## create empty byte buffer

```go
    buf := EmptyByteBuf()
```

## write string to byte buffer

```go
    buf.WriteString("abcdef")
```

## write int64 value to byte buffer

```go
    buf.WriteInt64(int64(12345))
```

## make a clone of current readable bytes

```go
    clone := buf.Clone()
```

## read 8 bytes array as int64 value from byte buffer

```go
    buf := EmptyByteBuf()
    buf.WriteInt64(int64(12345)) // readable bytes len = 8
    v := buf.ReadInt64() // v = int64(12345)
```

## skip 2 bytes and read 3 bytes of byte buffer

```go
    bs := buf.Skip(2).ReadBytes(3)
```

## reset byte buffer

```go
    buf.Reset()
```