package litecrate

import (
	"unsafe"
)

const (
	FlagAutoGrow     uint8 = 0                               // Automatically grow buffer when a write would exceed capacity
	FlagManualGrow   uint8 = 1                               // Only grow buffer when Grow() is called explicitly, panic if a write would exceed capacity
	FlagGrowDouble   uint8 = 0                               // When growing, double the old capacity and add n
	FlagGrowExact    uint8 = 2                               // When growing, only grow to exactly accomodate specified length n
	FlagAutoDouble   uint8 = FlagAutoGrow | FlagGrowDouble   // Automatically grow buffer by double+n when a write would exceed capacity
	FlagAutoExact    uint8 = FlagAutoGrow | FlagGrowExact    // Automatically grow buffer to exact length when a write would exceed capacity
	FlagManualDouble uint8 = FlagManualGrow | FlagGrowDouble // Only grow buffer by double+n when Grow() is called explicitly, panic if a write would exceed capacity
	FlagManualExact  uint8 = FlagManualGrow | FlagGrowExact  // Only grow buffer to exact length when Grow() is called explicitly, panic if a write would exceed capacity
	FlagDefault      uint8 = FlagAutoDouble                  // Automatically grow buffer by double+n when a write would exceed capacity
)

// Determines how the Access____() functions handle the variables passed to them
type AccessMode uint8

const (
	Write   AccessMode = 0 // Write value to Crate
	Read    AccessMode = 1 // Read value from crate
	Peek    AccessMode = 2 // Read value from Crate without advancing read index
	Discard AccessMode = 3 // Advance read index without using value
)

// Implementers of SelfAccessor indicate that if given a Crate and an AccessMode,
// they know how to call the correct methods to read/write themselves to/from it.
//
// It is generally preferable to call
//	crate.AccessSelfAccessor(selfAccessor, mode)
// rather than
//	selfAccessor.AccessSelf(crate, mode)
// as the former will correctly handle Peek mode without additional work inside
// user's definition of AccessSelf()
type SelfAccessor interface {
	AccessSelf(crate *Crate, mode AccessMode)
}

// A Crate is a data buffer with a separate read and write index
// and options for how it should grow when needed.
type Crate struct {
	data  []byte
	write uint64
	read  uint64
	flags uint8
}

// Just in case you want to pack Crates inside other Crates...
func (c *Crate) AccessSelf(crate *Crate, mode AccessMode) {
	c.AccessU64(&c.write, mode)
	c.AccessU64(&c.read, mode)
	c.AccessU8(&c.flags, mode)
	c.AccessBytesWithCounter(&c.data, mode)
}

// Create new Crate with specified initial size and option flags
func NewCrate(size uint64, flags uint8) *Crate {
	return &Crate{
		write: 0,
		read:  0,
		flags: flags,
		data:  make([]byte, size),
	}
}

// Create a new Crate from existing byte slice and option flags
func OpenCrate(data []byte, flags uint8) *Crate {
	return &Crate{
		write: uint64(len(data)),
		read:  0,
		flags: flags,
		data:  data,
	}
}

// Check whether a write of 'size' bytes will succeed.
// Grows buffer if crate was flagged with 'FlagAutoGrow' (default).
// Panics if not flagged for AutoGrow and 'size' would exceed capacity
func (c *Crate) CheckWrite(size uint64) {
	sum := c.write + size
	l64 := len64(c.data)
	if sum > l64 {
		if !c.WillAutoGrow() {
			panic("LiteCrate: AutoGrow set to false and cannot write " + intStr(size) + " more bytes (written bytes: " + intStr(c.write) + ", max bytes: " + intStr(l64) + ", space left: " + intStr(l64-c.write) + ")")
		}
		diff := sum - l64
		c.Grow(int(diff))
	}
	_ = c.data[sum-1]
}

// Check whether a read of 'size' bytes will succeed.
// Panics if 'size' would cause the read index to exceed the write index
func (c *Crate) CheckRead(size uint64) {
	sum := c.read + size
	if sum > c.write {
		panic("LiteCrate: cannot read " + intStr(size) + " more bytes (read index: " + intStr(c.read) + ", write index: " + intStr(c.write) + ", unread bytes left in crate: " + intStr(c.write-c.read) + ")")
	}
	_ = c.data[sum-1]
}

// Returns whether AutoGrow is set on Crate (default)
func (c *Crate) WillAutoGrow() bool {
	return c.flags&FlagManualGrow == 0
}

// Returns whether GrowMode is Double (default))
func (c *Crate) WillDoubleOnAllocate() bool {
	return c.flags&FlagGrowExact == 0
}

// Returns the length of the crate's written byte slice
func (c *Crate) Len() int {
	return int(c.write)
}

// Returns the capacity of the crate's byte slice
func (c *Crate) Cap() int {
	return cap(c.data)
}

// Grows the buffer by at least n bytes.
// Negative values allowed if you wish to Shrink.
// WILL NOT warn or panic if shrunk below end of written data.
// When Crate is flagged with FlagGrowExact, buffer will grow only to the exact size
// specified, otherwise it will grow to be double+n
func (c *Crate) Grow(n int) {
	switch {
	case n == 0:
		return
	case n < 0:
		if -n > len(c.data) {
			n = -len(c.data)
		}
		c.data = c.data[0 : len(c.data)+n]
		l64 := len64(c.data)
		if c.write > l64 {
			c.write = l64
		}
		if c.read > c.write {
			c.read = c.write
		}
	case len(c.data)+n <= cap(c.data):
		c.data = c.data[0 : len(c.data)+n]
	default:
		var alloc []byte
		switch {
		case c.WillDoubleOnAllocate():
			alloc = make([]byte, (len(c.data)*2)+n)
		default:
			alloc = make([]byte, len(c.data)+n)
		}
		copy(alloc, c.data)
		c.data = alloc
	}
}

// Returns a slice of the crate's written data
func (c *Crate) Data() []byte {
	b := c.data[:c.write]
	return b
}

// Returns a COPY of the crate's written data
func (c *Crate) DataCopy() []byte {
	bytes := make([]byte, c.write)
	copy(bytes, c.data[:c.write])
	return bytes
}

// Returns whether the data in one crate equals another
func (c *Crate) DataEqual(other *Crate) bool {
	equal := true
	lenA, lenB := len64(c.data), len64(other.data)
	if lenA != lenB {
		equal = false
	}
	for i := uint64(0); equal && i < lenA; i += 1 {
		if c.data[i] != other.data[i] {
			equal = false
		}
	}
	return equal
}

// Copies bytes from read index into dst, same as copy(dst, crate.data[readIndex:])
func (c *Crate) CopyTo(dst []byte) int {
	return copy(dst, c.data[c.read:])
}

// Copies bytes from src into write index, same as copy(crate.data[writeIndex:], src)
func (c *Crate) CopyFrom(src []byte) int {
	return copy(c.data[c.write:], src)
}

// Returns a separate but identical copy of the Crate, flags and read/write indexes included.
func (c *Crate) Clone() *Crate {
	crate := &Crate{
		data:  make([]byte, len(c.data), cap(c.data)),
		write: c.write,
		read:  c.read,
		flags: c.flags,
	}
	copy(crate.data, c.data)
	return crate
}

// Reverts crate to a "like-new" state without re-allocating underlying array.
// Useful if recycling large pre-allocated crates
func (c *Crate) Reset() {
	c.write = 0
	c.read = 0
}

// Reverts crate to a "like-new" state without re-allocating underlying array,
// while also setting all bytes to 0.
// Useful if recycling large pre-allocated crates
func (c *Crate) FullClear() {
	c.Reset()
	if len(c.data) == 0 {
		return
	}
	c.data[0] = 0
	for i := 1; i < len(c.data); i *= 2 {
		copy(c.data[i:], c.data[:i])
	}
}

// Reverts crate to a state where none of the data has been read yet but the write index remains the same.
func (c *Crate) ResetReadIndex() {
	c.read = 0
}

// Returns the current write index of the crate
func (c *Crate) WriteIndex() uint64 {
	return c.write
}

// Sets the current write index of the crate.
// If index is greater than capacity and AutoGrow is flagged it will grow the buffer,
// if not it will panic
func (c *Crate) SetWriteIndex(index uint64) {
	c.write = 0
	c.CheckWrite(index)
	c.write = index
}

// Returns the current read index of the Crate
func (c *Crate) ReadIndex() uint64 {
	return c.read
}

// Sets the current read index of the Crate.
// Will panic if read index exceeds write index
func (c *Crate) SetReadIndex(index uint64) {
	c.read = 0
	c.CheckRead(index)
	c.read = index
}

// Returns the number of bytes left for the Crate to write to,
// not accounting for any future Grows
func (c *Crate) SpaceLeft() uint64 {
	return len64(c.data) - c.write
}

// Returns the number of bytes left for the Crate to read from
func (c *Crate) ReadsLeft() uint64 {
	return c.write - c.read
}

// Set option flags for Crate
func (c *Crate) SetFlags(flags uint8) {
	c.flags = flags
}

// Advance read index n bytes without using them
func (c *Crate) DiscardN(n uint64) {
	c.read += n
	if c.read > c.write {
		c.read = c.write
	}
}

/**************
	BOOL
***************/

type isBool interface{ ~bool }

// Read bool from data into dst
func ReadBool[T isBool](data []byte, dst *T) {
	*(*uint8)(unsafe.Pointer(dst)) = data[0]
}

// Write bool from src into data
func WriteBool[T isBool](data []byte, src T) {
	data[0] = *(*uint8)(unsafe.Pointer(&src))
}

// Discard next unread byte in crate
func (c *Crate) DiscardBool() {
	c.DiscardN(1)
}

// Write bool to crate
func (c *Crate) WriteBool(val bool) {
	c.CheckWrite(1)
	WriteBool(c.data[c.write:], val)
	c.write += 1
}

// Read next byte from crate as bool
func (c *Crate) ReadBool() (val bool) {
	c.CheckRead(1)
	ReadBool(c.data[c.read:], &val)
	c.read += 1
	return val
}

// Read next byte from crate as bool without advancing read index
func (c *Crate) PeekBool() (val bool) {
	c.CheckRead(1)
	ReadBool(c.data[c.read:], &val)
	return val
}

// Use the bool pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessBool(val *bool, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteBool(*val)
	case Read:
		*val = c.ReadBool()
	case Peek:
		*val = c.PeekBool()
	case Discard:
		c.DiscardBool()
	default:
		panic("LiteCrate: Invalid mode passed to AccessBool()")
	}
}

/**************
	UINT8/BYTE
***************/

type isU8 interface{ ~uint8 }

// Read uint8 from data into dst
func ReadU8[T isU8](data []byte, dst *T) {
	*(*uint8)(unsafe.Pointer(dst)) = data[0]
}

// Write uint8 from src into data
func WriteU8[T isU8](data []byte, src T) {
	data[0] = *(*uint8)(unsafe.Pointer(&src))
}

// Discard next unread byte in crate
func (c *Crate) DiscardU8() {
	c.DiscardN(1)
}

// Write uint8 to crate
func (c *Crate) WriteU8(val uint8) {
	c.CheckWrite(1)
	WriteU8(c.data[c.write:], val)
	c.write += 1
}

// Read next byte from crate as uint8
func (c *Crate) ReadU8() (val uint8) {
	c.CheckRead(1)
	ReadU8(c.data[c.read:], &val)
	c.read += 1
	return val
}

// Read next byte from crate as uint8 without advancing read index
func (c *Crate) PeekU8() (val uint8) {
	c.CheckRead(1)
	ReadU8(c.data[c.read:], &val)
	return val
}

// Use the uint8 pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU8(val *uint8, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU8(*val)
	case Read:
		*val = c.ReadU8()
	case Peek:
		*val = c.PeekU8()
	case Discard:
		c.DiscardU8()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU8()")
	}
}

// Read byte from data into dst
func ReadByte[T isU8](data []byte, dst *T) {
	*(*uint8)(unsafe.Pointer(dst)) = data[0]
}

// Write byte from src into data
func WriteByte[T isU8](data []byte, src T) {
	data[0] = *(*uint8)(unsafe.Pointer(&src))
}

// Discard next unread byte in crate
func (c *Crate) DiscardByte() {
	c.DiscardN(1)
}

// Write byte to crate
func (c *Crate) WriteByte(val uint8) {
	c.WriteU8(val)
}

// Read next byte from crate
func (c *Crate) ReadByte() (val uint8) {
	return c.ReadU8()
}

// Read next byte from crate without advancing read index
func (c *Crate) PeekByte() (val uint8) {
	return c.PeekU8()
}

// Use the byte pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessByte(val *uint8, mode AccessMode) {
	c.AccessU8(val, mode)
}

/**************
	INT8
***************/

type isI8 interface{ ~int8 }

// Read int8 from data into dst
func ReadI8[T isI8](data []byte, dst *T) {
	*(*uint8)(unsafe.Pointer(dst)) = data[0]
}

// Write int8 from src into data
func WriteI8[T isI8](data []byte, src T) {
	data[0] = *(*uint8)(unsafe.Pointer(&src))
}

// Discard next unread byte in crate
func (c *Crate) DiscardI8() {
	c.DiscardN(1)
}

// Write int8 to crate
func (c *Crate) WriteI8(val int8) {
	c.CheckWrite(1)
	WriteI8(c.data[c.write:], val)
	c.write += 1
}

// Read next byte from crate as int8
func (c *Crate) ReadI8() (val int8) {
	c.CheckRead(1)
	ReadI8(c.data[c.read:], &val)
	c.read += 1
	return val
}

// Read next byte from crate as int8 without advancing read index
func (c *Crate) PeekI8() (val int8) {
	c.CheckRead(1)
	ReadI8(c.data[c.read:], &val)
	return val
}

// Use the int8 pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI8(val *int8, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI8(*val)
	case Read:
		*val = c.ReadI8()
	case Peek:
		*val = c.PeekI8()
	case Discard:
		c.DiscardI8()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI8()")
	}
}

/**************
	UINT16
***************/

type isU16 interface{ ~uint16 }

// Read uint16 from data into dst
func ReadU16[T isU16](data []byte, dst *T) {
	_ = data[1]
	*(*uint16)(unsafe.Pointer(dst)) = ( //
	/**/ uint16(data[0]) |
		uint16(data[1])<<8)
}

// Write uint16 from src into data
func WriteU16[T isU16](data []byte, src T) {
	val := *(*uint16)(unsafe.Pointer(&src))
	_ = data[1]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
}

// Discard next 2 unread bytes in crate
func (c *Crate) DiscardU16() {
	c.DiscardN(2)
}

// Write uint16 to crate
func (c *Crate) WriteU16(val uint16) {
	c.CheckWrite(2)
	WriteU16(c.data[c.write:], val)
	c.write += 2
}

// Read next 2 bytes from crate as uint16
func (c *Crate) ReadU16() (val uint16) {
	c.CheckRead(2)
	ReadU16(c.data[c.read:], &val)
	c.read += 2
	return val
}

// Read next 2 bytes from crate as uint16 without advancing read index
func (c *Crate) PeekU16() (val uint16) {
	c.CheckRead(2)
	ReadU16(c.data[c.read:], &val)
	return val
}

// Use the uint16 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU16(val *uint16, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU16(*val)
	case Read:
		*val = c.ReadU16()
	case Peek:
		*val = c.PeekU16()
	case Discard:
		c.DiscardU16()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU16()")
	}
}

/**************
	INT16
***************/

type isI16 interface{ ~int16 }

// Read int16 from data into dst
func ReadI16[T isI16](data []byte, dst *T) {
	_ = data[1]
	*(*uint16)(unsafe.Pointer(dst)) = ( //
	/**/ uint16(data[0]) |
		uint16(data[1])<<8)
}

// Write int16 from src into data
func WriteI16[T isI16](data []byte, src T) {
	val := *(*uint16)(unsafe.Pointer(&src))
	_ = data[1]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
}

// Discard next 2 unread bytes in crate
func (c *Crate) DiscardI16() {
	c.DiscardN(2)
}

// Write int16 to crate
func (c *Crate) WriteI16(val int16) {
	c.CheckWrite(2)
	WriteI16(c.data[c.write:], val)
	c.write += 2
}

// Read next 2 bytes from crate as int16
func (c *Crate) ReadI16() (val int16) {
	c.CheckRead(2)
	ReadI16(c.data[c.read:], &val)
	c.read += 2
	return val
}

// Read next 2 bytes from crate as int16 without advancing read index
func (c *Crate) PeekI16() (val int16) {
	c.CheckRead(2)
	ReadI16(c.data[c.read:], &val)
	return val
}

// Use the int16 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI16(val *int16, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI16(*val)
	case Read:
		*val = c.ReadI16()
	case Peek:
		*val = c.PeekI16()
	case Discard:
		c.DiscardI16()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI16()")
	}
}

/**************
	UINT24
***************/

type isU32 interface{ ~uint32 }

// Read 3 bytes from data into dst as a uint32
// where the value is known to always be VALUE <= 16777215
func ReadU24[T isU32](data []byte, dst *T) {
	_ = data[2]
	*(*uint32)(unsafe.Pointer(dst)) = ( //
	/**/ uint32(data[0]) |
		uint32(data[1])<<8 |
		uint32(data[2])<<16)
}

// Write uint32 from src into data as 3 bytes,
// where the value is known to always be VALUE <= 16777215
func WriteU24[T isU32](data []byte, src T) {
	val := *(*uint32)(unsafe.Pointer(&src))
	_ = data[2]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
}

// Discard next 3 unread bytes in crate
func (c *Crate) DiscardU24() {
	c.DiscardN(3)
}

// Write uint32 to crate as 3 bytes,
// where the value is known to always be VALUE <= 16777215
func (c *Crate) WriteU24(val uint32) {
	c.CheckWrite(3)
	WriteU24(c.data[c.write:], val)
	c.write += 3
}

// Read next 3 bytes from crate as uint32,
// where the value is known to always be VALUE <= 16777215
func (c *Crate) ReadU24() (val uint32) {
	c.CheckRead(3)
	ReadU24(c.data[c.read:], &val)
	c.read += 3
	return val
}

// Read next 3 bytes from crate as uint32 without advancing read index,
// where the value is known to always be VALUE <= 16777215
func (c *Crate) PeekU24() (val uint32) {
	c.CheckRead(3)
	ReadU24(c.data[c.read:], &val)
	return val
}

// Use the uint32 (VALUE <= 16777215 as 3 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU24(val *uint32, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU24(*val)
	case Read:
		*val = c.ReadU24()
	case Peek:
		*val = c.PeekU24()
	case Discard:
		c.DiscardU24()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU24()")
	}
}

/**************
	INT24
***************/

type isI32 interface{ ~int32 }

// Read 3 bytes from data into dst as an int32
// where the value is known to always be -8388608 <= VALUE <= 8388607
func ReadI24[T isI32](data []byte, dst *T) {
	_ = data[2]
	val := ( //
	/**/ uint32(data[0]) |
		uint32(data[1])<<8 |
		uint32(data[2])<<16)
	if val&minI24p > 0 {
		if val^minI24p == 0 {
			*(*uint32)(unsafe.Pointer(dst)) = minI24u
			return
		}
		val = (((val ^ maxU24) + 1) ^ maxU32) + 1
	}
	*(*uint32)(unsafe.Pointer(dst)) = val
}

// Write int32 from src into data as 3 bytes,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func WriteI24[T isI32](data []byte, src T) {
	val := *(*uint32)(unsafe.Pointer(&src))
	if val < 0 {
		val = (((val ^ maxU32) + 1) ^ maxU24) + 1
	}
	_ = data[2]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)

}

// Discard next 3 unread bytes in crate
func (c *Crate) DiscardI24() {
	c.DiscardN(3)
}

// Write int32 to crate as 3 bytes,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func (c *Crate) WriteI24(val int32) {
	c.CheckWrite(3)
	WriteI24(c.data[c.write:], val)
	c.write += 3
}

// Read next 3 bytes from crate as int32,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func (c *Crate) ReadI24() (val int32) {
	c.CheckRead(3)
	ReadI24(c.data[c.read:], &val)
	c.read += 3
	return val
}

// Read next 3 bytes from crate as int32 without advancing read index,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func (c *Crate) PeekI24() (val int32) {
	c.CheckRead(3)
	ReadI24(c.data[c.read:], &val)
	return val
}

// Use the int32 (-8388608 <= VALUE <= 8388607 as 3 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI24(val *int32, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI24(*val)
	case Read:
		*val = c.ReadI24()
	case Peek:
		*val = c.PeekI24()
	case Discard:
		c.DiscardI24()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI24()")
	}
}

/**************
	UINT32
***************/

// Read uint32 from data into dst
func ReadU32[T isU32](data []byte, dst *T) {
	_ = data[3]
	*(*uint32)(unsafe.Pointer(dst)) = ( //
	/**/ uint32(data[0]) |
		uint32(data[1])<<8 |
		uint32(data[2])<<16 |
		uint32(data[3])<<24)
}

// Write uint32 from src into data
func WriteU32[T isU32](data []byte, src T) {
	val := *(*uint32)(unsafe.Pointer(&src))
	_ = data[3]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
}

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardU32() {
	c.DiscardN(4)
}

// Write uint32 to crate
func (c *Crate) WriteU32(val uint32) {
	c.CheckWrite(4)
	WriteU32(c.data[c.write:], val)
	c.write += 4
}

// Read next 4 bytes from crate as uint32
func (c *Crate) ReadU32() (val uint32) {
	c.CheckRead(4)
	ReadU32(c.data[c.read:], &val)
	c.read += 4
	return val
}

// Read next 4 bytes from crate as uint32 without advancing read index
func (c *Crate) PeekU32() (val uint32) {
	c.CheckRead(4)
	ReadU32(c.data[c.read:], &val)
	return val
}

// Use the uint32 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU32(val *uint32, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU32(*val)
	case Read:
		*val = c.ReadU32()
	case Peek:
		*val = c.PeekU32()
	case Discard:
		c.DiscardU32()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU32()")
	}
}

/**************
	INT32/RUNE
***************/

// Read int32 from data into dst
func ReadI32[T isI32](data []byte, dst *T) {
	_ = data[3]
	*(*uint32)(unsafe.Pointer(dst)) = ( //
	/**/ uint32(data[0]) |
		uint32(data[1])<<8 |
		uint32(data[2])<<16 |
		uint32(data[3])<<24)
}

// Write int32 from src into data
func WriteI32[T isI32](data []byte, src T) {
	val := *(*uint32)(unsafe.Pointer(&src))
	_ = data[3]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
}

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardI32() {
	c.DiscardN(4)
}

// Write int32 to crate
func (c *Crate) WriteI32(val int32) {
	c.CheckWrite(4)
	WriteI32(c.data[c.write:], val)
	c.write += 4
}

// Read next 4 bytes from crate as int32
func (c *Crate) ReadI32() (val int32) {
	c.CheckRead(4)
	ReadI32(c.data[c.read:], &val)
	c.read += 4
	return val
}

// Read next 4 bytes from crate as int32 without advancing read index
func (c *Crate) PeekI32() (val int32) {
	c.CheckRead(4)
	ReadI32(c.data[c.read:], &val)
	return val
}

// Use the int32 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI32(val *int32, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI32(*val)
	case Read:
		*val = c.ReadI32()
	case Peek:
		*val = c.PeekI32()
	case Discard:
		c.DiscardI32()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI32()")
	}
}

// Read rune from data into dst
func ReadRune[T isI32](data []byte, dst *T) {
	ReadI32(data, dst)
}

// Write rune from src into data
func WriteRune[T isI32](data []byte, src T) {
	WriteI32(data, src)
}

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardRune() {
	c.DiscardN(4)
}

// Write rune to crate
func (c *Crate) WriteRune(val rune) {
	c.WriteRune(val)
}

// Read next 4 bytes from crate as rune
func (c *Crate) ReadRune() (val rune) {
	return c.ReadI32()
}

// Read next 4 bytes from crate as rune without advancing read index
func (c *Crate) PeekRune() (val rune) {
	return c.PeekI32()
}

// Use the rune pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessRune(val *rune, mode AccessMode) {
	c.AccessI32(val, mode)
}

/**************
	UINT40
***************/

type isU64 interface{ ~uint64 }

// Read 5 bytes from data into dst as a uint64
// where the value is known to always be VALUE <= 1099511627775
func ReadU40[T isU64](data []byte, dst *T) {
	_ = data[4]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32)
}

// Write uint64 from src into data as 5 bytes,
// where the value is known to always be VALUE <= 1099511627775
func WriteU40[T isU64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[4]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
}

// Discard next 5 unread bytes in crate
func (c *Crate) DiscardU40() {
	c.DiscardN(5)
}

// Write uint64 to crate as 5 bytes,
// where the value is known to always be VALUE <= 1099511627775
func (c *Crate) WriteU40(val uint64) {
	c.CheckWrite(5)
	WriteU40(c.data[c.write:], val)
	c.write += 5
}

// Read next 5 bytes from crate as uint64,
// where the value is known to always be VALUE <= 1099511627775
func (c *Crate) ReadU40() (val uint64) {
	c.CheckRead(5)
	ReadU40(c.data[c.read:], &val)
	c.read += 5
	return val
}

// Read next 5 bytes from crate as uint64 without advancing read index,
// where the value is known to always be VALUE <= 1099511627775
func (c *Crate) PeekU40() (val uint64) {
	c.CheckRead(5)
	ReadU40(c.data[c.read:], &val)
	return val
}

// Use the uint64 (VALUE <= 1099511627775 as 5 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU40(val *uint64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU40(*val)
	case Read:
		*val = c.ReadU40()
	case Peek:
		*val = c.PeekU40()
	case Discard:
		c.DiscardU40()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU40()")
	}
}

/**************
	INT40
***************/

type isI64 interface{ ~int64 }

// Read 5 bytes from data into dst as an int64
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func ReadI40[T isI64](data []byte, dst *T) {
	_ = data[4]
	val := ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32)
	if val&minI40p > 0 {
		if val^minI40p == 0 {
			*(*uint64)(unsafe.Pointer(dst)) = minI40u
			return
		}
		val = (((val ^ maxU40) + 1) ^ maxU64) + 1
	}
	*(*uint64)(unsafe.Pointer(dst)) = val
}

// Write int64 from src into data as 5 bytes,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func WriteI40[T isI64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	if val < 0 {
		val = (((val ^ maxU64) + 1) ^ maxU40) + 1
	}
	_ = data[4]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
}

// Discard next 5 unread bytes in crate
func (c *Crate) DiscardI40() {
	c.DiscardN(5)
}

// Write int64 to crate as 5 bytes,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func (c *Crate) WriteI40(val int64) {
	c.CheckWrite(5)
	WriteI40(c.data[c.write:], val)
	c.write += 5
}

// Read next 5 bytes from crate as int64,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func (c *Crate) ReadI40() (val int64) {
	c.CheckRead(5)
	ReadI40(c.data[c.read:], &val)
	c.read += 5
	return val
}

// Read next 5 bytes from crate as int64 without advancing read index,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func (c *Crate) PeekI40() (val int64) {
	c.CheckRead(5)
	ReadI40(c.data[c.read:], &val)
	return val
}

// Use the int64 (-549755813888 <= VALUE <= 549755813887 as 5 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI40(val *int64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI40(*val)
	case Read:
		*val = c.ReadI40()
	case Peek:
		*val = c.PeekI40()
	case Discard:
		c.DiscardI40()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI40()")
	}
}

/**************
	UINT48
***************/

// Read 6 bytes from data into dst as a uint64
// where the value is known to always be VALUE <= 281474976710655
func ReadU48[T isU64](data []byte, dst *T) {
	_ = data[5]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40)
}

// Write uint64 from src into data as 6 bytes,
// where the value is known to always be VALUE <= 281474976710655
func WriteU48[T isU64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[5]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
}

// Discard next 6 unread bytes in crate
func (c *Crate) DiscardU48() {
	c.DiscardN(6)
}

// Write uint64 to crate as 6 bytes,
// where the value is known to always be VALUE <= 281474976710655
func (c *Crate) WriteU48(val uint64) {
	c.CheckWrite(6)
	WriteU48(c.data[c.write:], val)
	c.write += 6
}

// Read next 6 bytes from crate as uint64,
// where the value is known to always be VALUE <= 281474976710655
func (c *Crate) ReadU48() (val uint64) {
	c.CheckRead(6)
	ReadU48(c.data[c.read:], &val)
	c.read += 6
	return val
}

// Read next 6 bytes from crate as uint64 without advancing read index,
// where the value is known to always be VALUE <= 281474976710655
func (c *Crate) PeekU48() (val uint64) {
	c.CheckRead(6)
	ReadU48(c.data[c.read:], &val)
	return val
}

// Use the uint64 (VALUE <= 281474976710655 as 6 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU48(val *uint64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU48(*val)
	case Read:
		*val = c.ReadU48()
	case Peek:
		*val = c.PeekU48()
	case Discard:
		c.DiscardU48()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU48()")
	}
}

/**************
	INT48
***************/

// Read 6 bytes from data into dst as an int64
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func ReadI48[T isI64](data []byte, dst *T) {
	_ = data[5]
	val := ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40)
	if val&minI48p > 0 {
		if val^minI48p == 0 {
			*(*uint64)(unsafe.Pointer(dst)) = minI48u
			return
		}
		val = (((val ^ maxU48) + 1) ^ maxU64) + 1
	}
	*(*uint64)(unsafe.Pointer(dst)) = val
}

// Write int64 from src into data as 6 bytes,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func WriteI48[T isI64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	if val < 0 {
		val = (((val ^ maxU64) + 1) ^ maxU48) + 1
	}
	_ = data[5]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
}

// Discard next 6 unread bytes in crate
func (c *Crate) DiscardI48() {
	c.DiscardN(6)
}

// Write int64 to crate as 6 bytes,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func (c *Crate) WriteI48(val int64) {
	c.CheckWrite(6)
	WriteI48(c.data[c.write:], val)
	c.write += 6
}

// Read next 6 bytes from crate as int64,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func (c *Crate) ReadI48() (val int64) {
	c.CheckRead(6)
	ReadI48(c.data[c.read:], &val)
	c.read += 6
	return val
}

// Read next 6 bytes from crate as int64 without advancing read index,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func (c *Crate) PeekI48() (val int64) {
	c.CheckRead(6)
	ReadI48(c.data[c.read:], &val)
	return val
}

// Use the int64 (-140737488355328 <= VALUE <= 140737488355327 as 6 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI48(val *int64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI48(*val)
	case Read:
		*val = c.ReadI48()
	case Peek:
		*val = c.PeekI48()
	case Discard:
		c.DiscardI48()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI48()")
	}
}

/**************
	UINT56
***************/

// Read 7 bytes from data into dst as a uint64
// where the value is known to always be VALUE <= 72057594037927935
func ReadU56[T isU64](data []byte, dst *T) {
	_ = data[6]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48)
}

// Write uint64 from src into data as 7 bytes,
// where the value is known to always be VALUE <= 72057594037927935
func WriteU56[T isU64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[6]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
}

// Discard next 7 unread bytes in crate
func (c *Crate) DiscardU56() {
	c.DiscardN(7)
}

// Write uint64 to crate as 7 bytes,
// where the value is known to always be VALUE <= 72057594037927935
func (c *Crate) WriteU56(val uint64) {
	c.CheckWrite(7)
	WriteU56(c.data[c.write:], val)
	c.write += 7
}

// Read next 7 bytes from crate as uint64,
// where the value is known to always be VALUE <= 72057594037927935
func (c *Crate) ReadU56() (val uint64) {
	c.CheckRead(7)
	ReadU56(c.data[c.read:], &val)
	c.read += 7
	return val
}

// Read next 7 bytes from crate as uint64 without advancing read index,
// where the value is known to always be VALUE <= 72057594037927935
func (c *Crate) PeekU56() (val uint64) {
	c.CheckRead(7)
	ReadU56(c.data[c.read:], &val)
	return val
}

// Use the uint64 (VALUE <= 72057594037927935 as 7 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU56(val *uint64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU56(*val)
	case Read:
		*val = c.ReadU56()
	case Peek:
		*val = c.PeekU56()
	case Discard:
		c.DiscardU56()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU56()")
	}
}

/**************
	INT56
***************/

// Read 7 bytes from data into dst as an int64
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func ReadI56[T isI64](data []byte, dst *T) {
	_ = data[6]
	val := ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48)
	if val&minI56p > 0 {
		if val^minI56p == 0 {
			*(*uint64)(unsafe.Pointer(dst)) = minI56u
			return
		}
		val = (((val ^ maxU56) + 1) ^ maxU64) + 1
	}
	*(*uint64)(unsafe.Pointer(dst)) = val
}

// Write int64 from src into data as 7 bytes,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func WriteI56[T isI64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	if val < 0 {
		val = (((val ^ maxU64) + 1) ^ maxU56) + 1
	}
	_ = data[6]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
}

// Discard next 7 unread bytes in crate
func (c *Crate) DiscardI56() {
	c.DiscardN(7)
}

// Write int64 to crate as 7 bytes,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func (c *Crate) WriteI56(val int64) {
	c.CheckWrite(7)
	WriteI56(c.data[c.write:], val)
	c.write += 7
}

// Read next 7 bytes from crate as int64,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func (c *Crate) ReadI56() (val int64) {
	c.CheckRead(7)
	ReadI56(c.data[c.read:], &val)
	c.read += 7
	return val
}

// Read next 7 bytes from crate as int64 without advancing read index,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func (c *Crate) PeekI56() (val int64) {
	c.CheckRead(7)
	ReadI56(c.data[c.read:], &val)
	return val
}

// Use the int64 (-36028797018963968 <= VALUE <= 36028797018963967 as 7 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI56(val *int64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI56(*val)
	case Read:
		*val = c.ReadI56()
	case Peek:
		*val = c.PeekI56()
	case Discard:
		c.DiscardI56()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI56()")
	}
}

/**************
	UINT64
***************/

// Read uint64 from data into dst
func ReadU64[T isU64](data []byte, dst *T) {
	_ = data[7]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
}

// Write uint64 from src into data
func WriteU64[T isU64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardU64() {
	c.DiscardN(8)
}

// Write uint64 to crate
func (c *Crate) WriteU64(val uint64) {
	c.CheckWrite(8)
	WriteU64(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as uint64
func (c *Crate) ReadU64() (val uint64) {
	c.CheckRead(8)
	ReadU64(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as uint64 without advancing read index
func (c *Crate) PeekU64() (val uint64) {
	c.CheckRead(8)
	ReadU64(c.data[c.read:], &val)
	return val
}

// Use the uint64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessU64(val *uint64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteU64(*val)
	case Read:
		*val = c.ReadU64()
	case Peek:
		*val = c.PeekU64()
	case Discard:
		c.DiscardU64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU64()")
	}
}

/**************
	INT64
***************/

// Read int64 from data into dst
func ReadI64[T isI64](data []byte, dst *T) {
	_ = data[7]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
}

// Write int64 from src into data
func WriteI64[T isI64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardI64() {
	c.DiscardN(8)
}

// Write int64 to crate
func (c *Crate) WriteI64(val int64) {
	c.CheckWrite(8)
	WriteI64(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as int64
func (c *Crate) ReadI64() (val int64) {
	c.CheckRead(8)
	ReadI64(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as int64 without advancing read index
func (c *Crate) PeekI64() (val int64) {
	c.CheckRead(8)
	ReadI64(c.data[c.read:], &val)
	return val
}

// Use the int64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessI64(val *int64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteI64(*val)
	case Read:
		*val = c.ReadI64()
	case Peek:
		*val = c.PeekI64()
	case Discard:
		c.DiscardI64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI64()")
	}
}

/**************
	INT
***************/

type isInt interface{ ~int }

// Read int from data into dst
func ReadInt[T isInt](data []byte, dst *T) {
	_ = data[7]
	val := ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
	*(*int)(unsafe.Pointer(dst)) = int(val)
}

// Write int from src into data
func WriteInt[T isInt](data []byte, src T) {
	valInt := int64(*(*int)(unsafe.Pointer(&src)))
	val := *(*uint64)(unsafe.Pointer(&valInt))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardInt() {
	c.DiscardN(8)
}

// Write int to crate
func (c *Crate) WriteInt(val int) {
	c.CheckWrite(8)
	WriteInt(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as int
func (c *Crate) ReadInt() (val int) {
	c.CheckRead(8)
	ReadInt(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as int without advancing read index
func (c *Crate) PeekInt() (val int) {
	c.CheckRead(8)
	ReadInt(c.data[c.read:], &val)
	return val
}

// Use the int pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessInt(val *int, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteInt(*val)
	case Read:
		*val = c.ReadInt()
	case Peek:
		*val = c.PeekInt()
	case Discard:
		c.DiscardInt()
	default:
		panic("LiteCrate: Invalid mode passed to AccessInt()")
	}
}

/**************
	UINT
***************/

type isUint interface{ ~uint }

// Read uint from data into dst
func ReadUint[T isUint](data []byte, dst *T) {
	_ = data[7]
	val := ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
	*(*uint)(unsafe.Pointer(dst)) = uint(val)
}

// Write uint from src into data
func WriteUint[T isUint](data []byte, src T) {
	val := uint64(*(*uint)(unsafe.Pointer(&src)))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardUint() {
	c.DiscardN(8)
}

// Write uint to crate
func (c *Crate) WriteUint(val uint) {
	c.CheckWrite(8)
	WriteUint(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as uint
func (c *Crate) ReadUint() (val uint) {
	c.CheckRead(8)
	ReadUint(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as uint without advancing read index
func (c *Crate) PeekUint() (val uint) {
	c.CheckRead(8)
	ReadUint(c.data[c.read:], &val)
	return val
}

// Use the uint pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessUint(val *uint, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteUint(*val)
	case Read:
		*val = c.ReadUint()
	case Peek:
		*val = c.PeekUint()
	case Discard:
		c.DiscardUint()
	default:
		panic("LiteCrate: Invalid mode passed to AccessUint()")
	}
}

/**************
	UINTPTR
***************/

type isUintPtr interface{ ~uintptr }

// Read uint from data into dst
func ReadUintPtr[T isUintPtr](data []byte, dst *T) {
	_ = data[7]
	val := ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
	*(*uintptr)(unsafe.Pointer(dst)) = uintptr(val)
}

// Write uintptr from src into data
func WriteUintPtr[T isUintPtr](data []byte, src T) {
	val := uint64(*(*uintptr)(unsafe.Pointer(&src)))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardUintPtr() {
	c.DiscardN(8)
}

// Write uintptr to crate
func (c *Crate) WriteUintPtr(val uintptr) {
	c.CheckWrite(8)
	WriteUintPtr(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as uintptr
func (c *Crate) ReadUintPtr() (val uintptr) {
	c.CheckRead(8)
	ReadUintPtr(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as uintptr without advancing read index
func (c *Crate) PeekUintPtr() (val uintptr) {
	c.CheckRead(8)
	ReadUintPtr(c.data[c.read:], &val)
	return val
}

// Use the uintptr pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessUintPtr(val *uintptr, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteUintPtr(*val)
	case Read:
		*val = c.ReadUintPtr()
	case Peek:
		*val = c.PeekUintPtr()
	case Discard:
		c.DiscardUintPtr()
	default:
		panic("LiteCrate: Invalid mode passed to AccessUintPtr()")
	}
}

/**************
	FLOAT32
***************/

type isF32 interface{ ~float32 }

// Read float32 from data into dst
func ReadF32[T isF32](data []byte, dst *T) {
	_ = data[3]
	*(*uint32)(unsafe.Pointer(dst)) = ( //
	/**/ uint32(data[0]) |
		uint32(data[1])<<8 |
		uint32(data[2])<<16 |
		uint32(data[3])<<24)
}

// Write float32 from src into data
func WriteF32[T isF32](data []byte, src T) {
	val := *(*uint32)(unsafe.Pointer(&src))
	_ = data[3]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
}

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardF32() {
	c.DiscardN(4)
}

// Write float32 to crate
func (c *Crate) WriteF32(val float32) {
	c.CheckWrite(4)
	WriteF32(c.data[c.write:], val)
	c.write += 4
}

// Read next 4 bytes from crate as float32
func (c *Crate) ReadF32() (val float32) {
	c.CheckRead(4)
	ReadF32(c.data[c.read:], &val)
	c.read += 4
	return val
}

// Read next 4 bytes from crate as float32 without advancing read index
func (c *Crate) PeekF32() (val float32) {
	c.CheckRead(4)
	ReadF32(c.data[c.read:], &val)
	return val
}

// Use the float32 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessF32(val *float32, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteF32(*val)
	case Read:
		*val = c.ReadF32()
	case Peek:
		*val = c.PeekF32()
	case Discard:
		c.DiscardF32()
	default:
		panic("LiteCrate: Invalid mode passed to AccessF32()")
	}
}

/**************
	FLOAT64
***************/

type isF64 interface{ ~float64 }

// Read float64 from data into dst
func ReadF64[T isF64](data []byte, dst *T) {
	_ = data[7]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
}

// Write float64 from src into data
func WriteF64[T isF64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardF64() {
	c.DiscardN(8)
}

// Write float64 to crate
func (c *Crate) WriteF64(val float64) {
	c.CheckWrite(8)
	WriteF64(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as float64
func (c *Crate) ReadF64() (val float64) {
	c.CheckRead(8)
	ReadF64(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as float64 without advancing read index
func (c *Crate) PeekF64() (val float64) {
	c.CheckRead(8)
	ReadF64(c.data[c.read:], &val)
	return val
}

// Use the float64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessF64(val *float64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteF64(*val)
	case Read:
		*val = c.ReadF64()
	case Peek:
		*val = c.PeekF64()
	case Discard:
		c.DiscardF64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessF64()")
	}
}

/**************
	COMPLEX64
***************/

type isC64 interface{ ~complex64 }

// Read complex64 from data into dst
func ReadC64[T isC64](data []byte, dst *T) {
	_ = data[7]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
}

// Write complex64 from src into data
func WriteC64[T isC64](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[7]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
}

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardC64() {
	c.DiscardN(8)
}

// Write complex64 to crate
func (c *Crate) WriteC64(val complex64) {
	c.CheckWrite(8)
	WriteC64(c.data[c.write:], val)
	c.write += 8
}

// Read next 8 bytes from crate as complex64
func (c *Crate) ReadC64() (val complex64) {
	c.CheckRead(8)
	ReadC64(c.data[c.read:], &val)
	c.read += 8
	return val
}

// Read next 8 bytes from crate as complex64 without advancing read index
func (c *Crate) PeekC64() (val complex64) {
	c.CheckRead(8)
	ReadC64(c.data[c.read:], &val)
	return val
}

// Use the complex64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessC64(val *complex64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteC64(*val)
	case Read:
		*val = c.ReadC64()
	case Peek:
		*val = c.PeekC64()
	case Discard:
		c.DiscardC64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessC64()")
	}
}

/**************
	COMPLEX128
***************/

type isC128 interface{ ~complex128 }

// Read complex128 from data into dst
func ReadC128[T isC128](data []byte, dst *T) {
	_ = data[15]
	*(*uint64)(unsafe.Pointer(dst)) = ( //
	/**/ uint64(data[0]) |
		uint64(data[1])<<8 |
		uint64(data[2])<<16 |
		uint64(data[3])<<24 |
		uint64(data[4])<<32 |
		uint64(data[5])<<40 |
		uint64(data[6])<<48 |
		uint64(data[7])<<56)
	*(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(dst)) + 8)) = ( //
	/**/ uint64(data[8]) |
		uint64(data[9])<<8 |
		uint64(data[10])<<16 |
		uint64(data[11])<<24 |
		uint64(data[12])<<32 |
		uint64(data[13])<<40 |
		uint64(data[14])<<48 |
		uint64(data[15])<<56)
}

// Write complex128 from src into data
func WriteC128[T isC128](data []byte, src T) {
	val := *(*uint64)(unsafe.Pointer(&src))
	_ = data[15]
	data[0] = byte(val)
	data[1] = byte(val >> 8)
	data[2] = byte(val >> 16)
	data[3] = byte(val >> 24)
	data[4] = byte(val >> 32)
	data[5] = byte(val >> 40)
	data[6] = byte(val >> 48)
	data[7] = byte(val >> 56)
	val = *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(&src)) + 8))
	data[8] = byte(val)
	data[9] = byte(val >> 8)
	data[10] = byte(val >> 16)
	data[11] = byte(val >> 24)
	data[12] = byte(val >> 32)
	data[13] = byte(val >> 40)
	data[14] = byte(val >> 48)
	data[15] = byte(val >> 56)
}

// Discard next 16 unread bytes in crate
func (c *Crate) DiscardC128() {
	c.DiscardN(16)
}

// Write complex128 to crate
func (c *Crate) WriteC128(val complex128) {
	c.CheckWrite(16)
	WriteC128(c.data[c.write:], val)
	c.write += 16
}

// Read next 16 bytes from crate as complex128
func (c *Crate) ReadC128() (val complex128) {
	c.CheckRead(16)
	ReadC128(c.data[c.read:], &val)
	c.read += 16
	return val
}

// Read next 16 bytes from crate as complex128 without advancing read index
func (c *Crate) PeekC128() (val complex128) {
	c.CheckRead(16)
	ReadC128(c.data[c.read:], &val)
	return val
}

// Use the complex128 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessC128(val *complex128, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteC128(*val)
	case Read:
		*val = c.ReadC128()
	case Peek:
		*val = c.PeekC128()
	case Discard:
		c.DiscardC128()
	default:
		panic("LiteCrate: Invalid mode passed to AccessC128()")
	}
}

/**************
	COUNTER
***************/

const (
	continueMask   = 128
	countMask      = 127
	finalCountMask = 255
	countShift1    = 0
	countShift2    = 7
	countShift3    = 14
	countShift4    = 21
	countShift5    = 28
	countShift6    = 35
	countShift7    = 42
	countShift8    = 49
	countShift9    = 56
)

// Read length counter (uint64) from data into dst,
// using 1-9 bytes dependant on length.
// Returns the number of bytes read and whether the value represents nil instead of zero
func ReadLength[T isU64](data []byte, dst *T) (bytesRead uint64, isNil bool) {
	b := data[0]
	var val uint64 = 0
	count := uint64(b & countMask)
	longer := (b & continueMask) > 0
	val |= count << countShift1
	if !longer {
		if val == 0 {
			*(*uint64)(unsafe.Pointer(dst)) = val
			return 1, true
		}
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 1, false
	}
	// a counter with more than 1 length byte means the buffer MUST hold AT LEAST 129 more bytes
	// we can eliminate the remaining bounds checks, guaranteed (unless the buffer is malformed)
	_ = data[8]
	b = data[1]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift2
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 2, false
	}
	b = data[2]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift3
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 3, false
	}
	b = data[3]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift4
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 4, false
	}
	b = data[4]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift5
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 5, false
	}
	b = data[5]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift6
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 6, false
	}
	b = data[6]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift7
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 7, false
	}
	b = data[7]
	count = uint64(b & countMask)
	longer = (b & continueMask) > 0
	val |= count << countShift8
	if !longer {
		*(*uint64)(unsafe.Pointer(dst)) = val - 1
		return 8, false
	}
	b = data[8]
	count = uint64(b & finalCountMask)
	val |= count << countShift9
	*(*uint64)(unsafe.Pointer(dst)) = val - 1
	return 9, false
}

// Write length counter (uint64) from src into data, with special behavior for representing nil.
// Uses 1-9 bytes dependant on length, returns number of bytes written
func WriteLength[T isU64](data []byte, src T, isNil bool) (bytesWritten uint64) {
	if isNil {
		data[0] = 0
		return 1
	}
	val := *(*uint64)(unsafe.Pointer(&src)) + 1
	count := (val & countMask)
	val = val >> countShift2
	longer := val > 0
	b := count
	if longer {
		b |= continueMask
	}
	data[0] = byte(b)
	if !longer {
		return 1
	}
	count = (val & countMask)
	val = val >> countShift3
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[1] = byte(b)
	if !longer {
		return 2
	}
	count = (val & countMask)
	val = val >> countShift4
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[2] = byte(b)
	if !longer {
		return 3
	}
	count = (val & countMask)
	val = val >> countShift5
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[3] = byte(b)
	if !longer {
		return 4
	}
	count = (val & countMask)
	val = val >> countShift6
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[4] = byte(b)
	if !longer {
		return 5
	}
	count = (val & countMask)
	val = val >> countShift7
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[5] = byte(b)
	if !longer {
		return 6
	}
	count = (val & countMask)
	val = val >> countShift8
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[6] = byte(b)
	if !longer {
		return 7
	}
	count = (val & countMask)
	val = val >> countShift9
	longer = val > 0
	b = count
	if longer {
		b |= continueMask
	}
	data[7] = byte(b)
	if !longer {
		return 8
	}
	data[8] = byte(val)
	return 9
}

func findLengthBytesFromData(data []byte) uint64 {
	longer := (data[0] & continueMask) > 0
	if !longer {
		return 1
	}
	// a counter with more than 1 length byte means the crate MUST hold AT LEAST 129 more bytes
	// we can eliminate the remaining bounds checks, guaranteed (unless the buffer is malformed)
	_ = data[8]
	longer = (data[1] & continueMask) > 0
	if !longer {
		return 2
	}
	longer = (data[2] & continueMask) > 0
	if !longer {
		return 3
	}
	longer = (data[3] & continueMask) > 0
	if !longer {
		return 4
	}
	longer = (data[4] & continueMask) > 0
	if !longer {
		return 5
	}
	longer = (data[5] & continueMask) > 0
	if !longer {
		return 6
	}
	longer = (data[6] & continueMask) > 0
	if !longer {
		return 7
	}
	longer = (data[7] & continueMask) > 0
	if !longer {
		return 8
	}
	return 9
}

func findLengthBytesFromValue(length uint64, isNil bool) uint64 {
	length += 1
	switch {
	case isNil:
		return 1
	case length <= 127:
		return 1
	case length <= 16383:
		return 2
	case length <= 2097151:
		return 3
	case length <= 268435455:
		return 4
	case length <= 34359738367:
		return 5
	case length <= 4398046511103:
		return 6
	case length <= 562949953421311:
		return 7
	case length <= 72057594037927935:
		return 8
	default:
		return 9
	}
}

// Discard next 1-9 unread bytes in crate,
// dependant on length of length counter
func (c *Crate) DiscardLength() (bytesDiscarded uint64) {
	n := findLengthBytesFromData(c.data[c.read:])
	c.DiscardN(n)
	return n
}

// Write length counter (uint64) to crate, with special behavior for representing nil.
// Uses 1-9 bytes dependant on length
func (c *Crate) WriteLength(length uint64, isNil bool) (bytesWritten uint64) {
	bytes := findLengthBytesFromValue(length, isNil)
	c.CheckWrite(bytes)
	WriteLength(c.data[c.write:], length, isNil)
	c.write += bytes
	return bytes
}

// Read next 1-9 bytes from crate as length counter (uint64),
// with a special indicator for representing nil
func (c *Crate) ReadLength() (length uint64, isNil bool, bytesRead uint64) {
	n := findLengthBytesFromData(c.data[c.read:])
	c.CheckRead(n)
	bytesRead, isNil = ReadLength(c.data[c.read:], &length)
	c.read += bytesRead
	return length, isNil, bytesRead
}

// Read next 1-9 bytes from crate as length counter (uint64) without advancing read index,
// with a special indicator for representing nil
func (c *Crate) PeekLength() (length uint64, isNil bool, bytesRead uint64) {
	n := findLengthBytesFromData(c.data[c.read:])
	c.CheckRead(n)
	bytesRead, isNil = ReadLength(c.data[c.read:], &length)
	return length, isNil, bytesRead
}

// Use the length counter (uint64) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessLength(val *uint64, writeNil bool, mode AccessMode) (readNil bool, bytesUsed uint64) {
	switch mode {
	case Write:
		bytesUsed = c.WriteLength(*val, writeNil)
	case Read:
		*val, readNil, bytesUsed = c.ReadLength()
	case Peek:
		*val, readNil, bytesUsed = c.PeekLength()
	case Discard:
		bytesUsed = c.DiscardLength()
	default:
		panic("LiteCrate: Invalid mode passed to AccessLength()")
	}
	return readNil, bytesUsed
}

/**************
	STRING
***************/

type isString interface{ ~string }

// Read string of specified byte length from data into dst,
func ReadString[T isString](data []byte, length uint64, dst *T) {
	if length == 0 {
		return
	}
	_ = data[length-1]
	bytes := make([]byte, length)
	copy(bytes, data[:length])
	targetPtr := (*stringInternals)(unsafe.Pointer(dst))
	targetPtr.data = (*sliceInternals)(unsafe.Pointer(&bytes)).data
	targetPtr.length = len(bytes)
}

// Read string with preceding length counter from data into dst
func ReadStringWithCounter[T isString](data []byte, dst *T) {
	var length, n uint64
	n, _ = ReadLength(data, &length)
	ReadString(data[n:], length, dst)
}

// Write string from src into data,
func WriteString[T isString](data []byte, src T) {
	length := uint64((*stringInternals)(unsafe.Pointer(&src)).length)
	_ = data[length]
	bytes := make([]byte, length, length)
	(*sliceInternals)(unsafe.Pointer(&bytes)).data = (*stringInternals)(unsafe.Pointer(&src)).data
	copy(data[:length], bytes)
}

// Write string with preceding length counter from src into data,
func WriteStringWithCounter[T isString](data []byte, src T) {
	length := uint64((*stringInternals)(unsafe.Pointer(&src)).length)
	start := WriteLength(data, length, false)
	_ = data[start+length]
	bytes := make([]byte, length, length)
	(*sliceInternals)(unsafe.Pointer(&bytes)).data = (*stringInternals)(unsafe.Pointer(&src)).data
	copy(data[start:start+length], bytes)
}

// Discard next unread string of specified length in crate
func (c *Crate) DiscardString(length uint64) {
	c.DiscardN(length)
}

// Discard next unread string with preceding length counter in crate
func (c *Crate) DiscardStringWithCounter() {
	var length, n uint64
	n, _ = ReadLength(c.data[c.read:], &length)
	c.DiscardN(length + n)
}

// Write string to crate
func (c *Crate) WriteString(val string) {
	length := len64str(val)
	c.CheckWrite(length)
	WriteString(c.data[c.write:], val)
	c.write += length
}

// Write string to crate with preceding length counter
func (c *Crate) WriteStringWithCounter(val string) {
	length := len64str(val)
	c.WriteLength(length, false)
	c.CheckWrite(length)
	WriteString(c.data[c.write:], val)
	c.write += length
}

// Read next string of specified byte length from crate
func (c *Crate) ReadString(length uint64) (val string) {
	c.CheckRead(length)
	ReadString(c.data[c.read:], length, &val)
	c.read += length
	return val
}

// Read next string with preceding length counter from crate
func (c *Crate) ReadStringWithCounter() (val string) {
	length, _, _ := c.ReadLength()
	c.CheckRead(length)
	ReadString(c.data[c.read:], length, &val)
	c.read += length
	return val
}

// Read next string of specified byte length from crate without advancing read index
func (c *Crate) PeekString(length uint64) (val string) {
	c.CheckRead(length)
	ReadString(c.data[c.read:], length, &val)
	return val
}

// Read next string with preceding length counter from crate without advancing read index
func (c *Crate) PeekStringWithCounter() (val string) {
	length, _, n := c.ReadLength()
	c.CheckRead(length)
	ReadString(c.data[c.read:], length, &val)
	c.read -= n
	return val
}

// Use the string pointed to by val according to mode (with specified read length):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessString(val *string, readLength uint64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteString(*val)
	case Read:
		*val = c.ReadString(readLength)
	case Peek:
		*val = c.PeekString(readLength)
	case Discard:
		c.DiscardString(readLength)
	default:
		panic("LiteCrate: Invalid mode passed to AccessString()")
	}
}

// Use the string pointed to by val according to mode (with length counter):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessStringWithCounter(val *string, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteStringWithCounter(*val)
	case Read:
		*val = c.ReadStringWithCounter()
	case Peek:
		*val = c.PeekStringWithCounter()
	case Discard:
		c.DiscardStringWithCounter()
	default:
		panic("LiteCrate: Invalid mode passed to AccessStringWithCounter()")
	}
}

/**************
	[]BYTE
***************/

type isByteSlice interface{ ~[]byte }

// Read byte slice of specified length from data into dst,
func ReadBytes[T isByteSlice](data []byte, length uint64, dst *T) {
	if length == 0 {
		return
	}
	_ = data[length-1]
	bytes := make([]byte, length)
	copy(bytes, data[:length])
	targetPtr := (*sliceInternals)(unsafe.Pointer(dst))
	targetPtr.data = (*sliceInternals)(unsafe.Pointer(&bytes)).data
	targetPtr.capacity = cap(bytes)
	targetPtr.length = len(bytes)
}

// Read byte slice with preceding length counter from data into dst
func ReadBytesWithCounter[T isByteSlice](data []byte, dst *T) {
	var length, n uint64
	isNil := false
	n, isNil = ReadLength(data, &length)
	if isNil {
		*(*[]byte)(unsafe.Pointer(dst)) = nil
		return
	}
	ReadBytes(data[n:], length, dst)
}

// Write byte slice from src into data,
func WriteBytes[T isByteSlice](data []byte, src T) {
	bytes := *(*[]byte)(unsafe.Pointer(&src))
	copy(data, bytes)
}

// Write byte slice with preceding length counter from src into data,
func WriteBytesWithCounter[T isString](data []byte, src T) {
	bytes := *(*[]byte)(unsafe.Pointer(&src))
	length := len64(bytes)
	isNil := data == nil
	start := WriteLength(data, length, isNil)
	if isNil || length == 0 {
		return
	}
	_ = data[start+length]
	copy(data[start:start+length], bytes)
}

// Discard next unread bytes of specified length in crate
func (c *Crate) DiscardBytes(length uint64) {
	c.DiscardN(length)
}

// Discard next unread bytes with preceding length counter in crate
func (c *Crate) DiscardBytesWithCounter() {
	var length, n uint64
	n, _ = ReadLength(c.data[c.read:], &length)
	c.DiscardN(length + n)
}

// Write bytes to crate
func (c *Crate) WriteBytes(val []byte) {
	length := len64(val)
	c.CheckWrite(length)
	WriteBytes(c.data[c.write:], val)
	c.write += length
}

// Write bytes to crate with preceding length counter
func (c *Crate) WriteBytesWithCounter(val []byte) {
	length := len64(val)
	isNil := val == nil
	c.WriteLength(length, isNil)
	if isNil || length == 0 {
		return
	}
	c.CheckWrite(length)
	WriteBytes(c.data[c.write:], val)
	c.write += length
}

// Read next bytes slice of specified length from crate
func (c *Crate) ReadBytes(length uint64) (val []byte) {
	c.CheckRead(length)
	ReadBytes(c.data[c.read:], length, &val)
	c.read += length
	return val
}

// Read next bytes slice with preceding length counter from crate
func (c *Crate) ReadBytesWithCounter() (val []byte) {
	length, isNil, _ := c.ReadLength()
	if isNil {
		return nil
	}
	c.CheckRead(length)
	ReadBytes(c.data[c.read:], length, &val)
	c.read += length
	return val
}

// Read next bytes slice of specified  length from crate without advancing read index
func (c *Crate) PeekBytes(length uint64) (val []byte) {
	c.CheckRead(length)
	ReadBytes(c.data[c.read:], length, &val)
	return val
}

// Read next bytes slice with preceding length counter from crate without advancing read index
func (c *Crate) PeekBytesWithCounter() (val []byte) {
	length, isNil, n := c.ReadLength()
	if isNil {
		c.read -= n
		return nil
	}
	c.CheckRead(length)
	ReadBytes(c.data[c.read:], length, &val)
	c.read -= n
	return val
}

// Use the []byte pointed to by val according to mode (with specified read length):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessBytes(val *[]byte, readLength uint64, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteBytes(*val)
	case Read:
		*val = c.ReadBytes(readLength)
	case Peek:
		*val = c.PeekBytes(readLength)
	case Discard:
		c.DiscardBytes(readLength)
	default:
		panic("LiteCrate: Invalid mode passed to AccessBytes()")
	}
}

// Use the []byte pointed to by val according to mode (with length counter):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
func (c *Crate) AccessBytesWithCounter(val *[]byte, mode AccessMode) {
	switch mode {
	case Write:
		c.WriteBytesWithCounter(*val)
	case Read:
		*val = c.ReadBytesWithCounter()
	case Peek:
		*val = c.PeekBytesWithCounter()
	case Discard:
		c.DiscardBytesWithCounter()
	default:
		panic("LiteCrate: Invalid mode passed to AccessBytesWithCounter()")
	}
}

/**************
	SelfAccessor
***************/

// Write SelfAccessor to crate
func (c *Crate) WriteSelfAccessor(val SelfAccessor) {
	val.AccessSelf(c, Write)
}

// Read next SelfAccessor from crate
func (c *Crate) ReadSelfAccessor(val SelfAccessor) {
	val.AccessSelf(c, Read)
}

// Read next SelfAccessor from crate without advancing read index
func (c *Crate) PeekSelfAccessor(val SelfAccessor) {
	indexBefore := c.read
	val.AccessSelf(c, Read)
	c.read = indexBefore
}

// Discard next SelfAccessor in crate
func (c *Crate) DiscardSelfAccessor(val SelfAccessor) {
	val.AccessSelf(c, Discard)
}

// Use SelfAccessor according to mode
func (c *Crate) AccessSelfAccessor(val SelfAccessor, mode AccessMode) {
	if mode == Peek {
		c.PeekSelfAccessor(val)
		return
	}
	val.AccessSelf(c, mode)
}

/**************
	SLICE/MAP
***************/

// Helper func for selectively reading/writing a slice of any type, dependant on mode.
// Automatically reads/writes a length counter, then uses accessElementFunc() in a loop
// to write each value. accessElementFunc() can be a
// custom function for more complex cases, or one of the predefined Access____() functions,
// assuming its signature matches the slice element type. For Read and Peek mode, a nil slice
// will be initialized to a non-nil slice of the needed length
//
// Example:
//	var myFloat64Slice = []float64{...}
//	var myCrate = NewCrate(1000, FlagAutoDouble)
//
//	AccessSlice(myCrate, Write, &myFloat64Slice, myCrate.SelectF64)
func AccessSlice[T any](crate *Crate, mode AccessMode, slice *[]T, accessElementFunc func(element *T, mode AccessMode)) {
	length := len64(*slice)
	writeNil := *slice == nil
	readNil, _ := crate.AccessLength(&length, writeNil, mode)
	switch mode {
	case Read, Peek, Discard:
		if readNil && mode != Discard {
			*slice = nil
			return
		}
		if *slice == nil && mode != Discard {
			*slice = make([]T, length)
		}
		for i := uint64(0); i < length; i += 1 {
			var elem T
			accessElementFunc(&elem, mode)
			if mode != Discard {
				(*slice)[i] = elem
			}
		}
	case Write:
		if writeNil {
			return
		}
		for i := uint64(0); i < length; i += 1 {
			accessElementFunc(&(*slice)[i], mode)
		}
	default:
		panic("LiteCrate: invalid mode passed to AccessSlice()")
	}
}

// Helper func for selectively reading/writing a map of any type, dependant on mode.
// Automatically reads/writes a length counter, then uses accessKeyFunc() and accessValFunc() in a loop
// to write each key-value pair adjacent to each other (key first, value second). accessKeyFunc() and accessValFunc() can be
// custom functions for more complex cases, or one of the predefined Access____() functions,
// assuming their signatures match the map key and value type. For Read and Peek mode, a nil map
// will be initialized to a non-nil map of the needed length
//
// Example:
//	var myStringIntMap = map[string]int{...}
//	var myCrate = NewCrate(1000, FlagAutoDouble)
//
//	AccessMap(myCrate, Write, &myStringIntMap, myCrate.AccessStringWithCounter, myCrate.SelectInt)
func AccessMap[K comparable, V any](crate *Crate, mode AccessMode, Map *map[K]V, accessKeyFunc func(key *K, mode AccessMode), accessValFunc func(val *V, mode AccessMode)) {
	mapLen := len64map(*Map)
	writeNil := *Map == nil
	readNil, _ := crate.AccessLength(&mapLen, writeNil, mode)
	switch mode {
	case Read, Peek, Discard:
		if readNil && mode != Discard {
			*Map = nil
			return
		}
		if *Map == nil && mode != Discard {
			*Map = make(map[K]V, mapLen)
		}
		for i := uint64(0); i < mapLen; i += 1 {
			var key K
			var val V
			accessKeyFunc(&key, mode)
			accessValFunc(&val, mode)
			if mode != Discard {
				(*Map)[key] = val
			}
		}
	case Write:
		if writeNil {
			return
		}
		for key, val := range *Map {
			accessKeyFunc(&key, mode)
			accessValFunc(&val, mode)
		}
	default:
		panic("LiteCrate: invalid mode passed to AccessMap()")
	}
}

/**************
	INTERNAL
***************/

const (
	maxU24  = 16777215
	maxI24  = 8388607
	minI24  = -8388608
	minI24p = 8388608
	minI24u = maxU32 - minI24p + 1

	maxU32 = 4294967295

	maxU40  = 1099511627775
	maxI40  = 549755813887
	minI40  = -549755813888
	minI40p = 549755813888
	minI40u = maxU64 - minI40p + 1

	maxU48  = 281474976710655
	maxI48  = 140737488355327
	minI48  = -140737488355328
	minI48p = 140737488355328
	minI48u = maxU64 - minI48p + 1

	maxU56  = 72057594037927935
	maxI56  = 36028797018963967
	minI56  = -36028797018963968
	minI56p = 36028797018963968
	minI56u = maxU64 - minI56p + 1

	maxU64 = 18446744073709551615
)

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

func len64[T any](slice []T) uint64 {
	return uint64(len(slice))
}
func cap64[T any](slice []T) uint64 {
	return uint64(cap(slice))
}
func len64str(str string) uint64 {
	return uint64(len(str))
}
func len64map[K comparable, V any](m map[K]V) uint64 {
	return uint64(len(m))
}

type sliceInternals struct {
	data     unsafe.Pointer
	length   int
	capacity int
}

type stringInternals struct {
	data   unsafe.Pointer
	length int
}

func intStr[T integer](val T) string {
	if val == 0 {
		return "0"
	}
	var data [21]uint8
	neg := false
	if val < 0 {
		neg = true
		val = -val
	}
	digit := 20
	for val >= 10 {
		div10 := val / 10
		data[digit] = uint8('0' + val - (div10 * 10))
		digit -= 1
		val = div10
	}
	data[digit] = uint8('0' + val)
	if neg {
		digit -= 1
		data[digit] = uint8('-')
	}
	return string(data[digit:])
}
