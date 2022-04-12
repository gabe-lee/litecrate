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
	Slice   AccessMode = 4 // get the byte slice the value occupies in crate without advancing read index
)

// Implementers of SelfAccessor indicate that if given a Crate and an AccessMode,
// they know how to call the correct methods to read/write themselves to/from it.
//
// It is generally preferable to call
//	crate.AccessSelfAccessor(selfAccessor, mode)
// rather than
//	selfAccessor.AccessSelf(crate, mode)
// as the former will correctly handle 'Peek' and 'Slice' modes without additional work inside
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

// Discard next unread byte in crate
func (c *Crate) DiscardBool() {
	c.DiscardN(1)
}

// Return byte slice the next unread bool occupies
func (c *Crate) SliceBool() (slice []byte) {
	c.CheckRead(1)
	return c.data[c.read : c.read+1 : c.read+1]
}

// Write bool to crate
func (c *Crate) WriteBool(val bool) {
	c.CheckWrite(1)
	c.data[c.write] = *(*uint8)(unsafe.Pointer(&val))
	c.write += 1
}

// Read next byte from crate as bool
func (c *Crate) ReadBool() (val bool) {
	val = c.PeekBool()
	c.read += 1
	return val
}

// Read next byte from crate as bool without advancing read index
func (c *Crate) PeekBool() (val bool) {
	c.CheckRead(1)
	val = *(*bool)(unsafe.Pointer(&c.data[c.read]))
	return val
}

// Use the bool pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessBool(val *bool, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteBool(*val)
	case Read:
		*val = c.ReadBool()
	case Peek:
		*val = c.PeekBool()
	case Discard:
		c.DiscardBool()
	case Slice:
		sliceModeData = c.SliceBool()
	default:
		panic("LiteCrate: Invalid mode passed to AccessBool()")
	}
	return sliceModeData
}

/**************
	UINT8/BYTE
***************/

// Discard next unread byte in crate
func (c *Crate) DiscardU8() {
	c.DiscardN(1)
}

// Return byte slice the next unread uint8 occupies
func (c *Crate) SliceU8() (slice []byte) {
	c.CheckRead(1)
	return c.data[c.read : c.read+1 : c.read+1]
}

// Write uint8 to crate
func (c *Crate) WriteU8(val uint8) {
	c.CheckWrite(1)
	c.data[c.write] = val
	c.write += 1
}

// Read next byte from crate as uint8
func (c *Crate) ReadU8() (val uint8) {
	val = c.PeekU8()
	c.read += 1
	return val
}

// Read next byte from crate as uint8 without advancing read index
func (c *Crate) PeekU8() (val uint8) {
	c.CheckRead(1)
	val = c.data[c.read]
	return val
}

// Use the uint8 pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU8(val *uint8, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU8(*val)
	case Read:
		*val = c.ReadU8()
	case Peek:
		*val = c.PeekU8()
	case Discard:
		c.DiscardU8()
	case Slice:
		sliceModeData = c.SliceU8()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU8()/AccessByte()")
	}
	return sliceModeData
}

// Discard next unread byte in crate
func (c *Crate) DiscardByte() {
	c.DiscardN(1)
}

// Return byte slice the next unread byte occupies
func (c *Crate) SliceByte() (slice []byte) {
	c.CheckRead(1)
	return c.data[c.read : c.read+1 : c.read+1]
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
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessByte(val *uint8, mode AccessMode) {
	c.AccessU8(val, mode)
}

/**************
	INT8
***************/

// Discard next unread byte in crate
func (c *Crate) DiscardI8() {
	c.DiscardN(1)
}

// Return byte slice the next unread int8 occupies
func (c *Crate) SliceI8() (slice []byte) {
	c.CheckRead(1)
	return c.data[c.read : c.read+1 : c.read+1]
}

// Write int8 to crate
func (c *Crate) WriteI8(val int8) {
	c.CheckWrite(1)
	c.data[c.write] = *(*uint8)(unsafe.Pointer(&val))
	c.write += 1
}

// Read next byte from crate as int8
func (c *Crate) ReadI8() (val int8) {
	val = c.PeekI8()
	c.read += 1
	return val
}

// Read next byte from crate as int8 without advancing read index
func (c *Crate) PeekI8() (val int8) {
	c.CheckRead(1)
	val = *(*int8)(unsafe.Pointer(&c.data[c.read]))
	return val
}

// Use the int8 pointed to by val according to mode,
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI8(val *int8, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI8(*val)
	case Read:
		*val = c.ReadI8()
	case Peek:
		*val = c.PeekI8()
	case Discard:
		c.DiscardI8()
	case Slice:
		sliceModeData = c.SliceI8()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI8()")
	}
	return sliceModeData
}

/**************
	UINT16
***************/

// Discard next 2 unread bytes in crate
func (c *Crate) DiscardU16() {
	c.DiscardN(2)
}

// Return byte slice the next unread uint16 occupies
func (c *Crate) SliceU16() (slice []byte) {
	c.CheckRead(2)
	return c.data[c.read : c.read+2 : c.read+2]
}

// Write uint16 to crate
func (c *Crate) WriteU16(val uint16) {
	c.CheckWrite(2)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.write += 2
}

// Read next 2 bytes from crate as uint16
func (c *Crate) ReadU16() (val uint16) {
	val = c.PeekU16()
	c.read += 2
	return val
}

// Read next 2 bytes from crate as uint16 without advancing read index
func (c *Crate) PeekU16() (val uint16) {
	c.CheckRead(2)
	val = ( //
	/**/ uint16(c.data[c.read+0]) |
		uint16(c.data[c.read+1])<<8)
	return val
}

// Use the uint16 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU16(val *uint16, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU16(*val)
	case Read:
		*val = c.ReadU16()
	case Peek:
		*val = c.PeekU16()
	case Discard:
		c.DiscardU16()
	case Slice:
		sliceModeData = c.SliceU16()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU16()")
	}
	return sliceModeData
}

/**************
	INT16
***************/

// Discard next 2 unread bytes in crate
func (c *Crate) DiscardI16() {
	c.DiscardN(2)
}

// Return byte slice the next unread int16 occupies
func (c *Crate) SliceI16() (slice []byte) {
	c.CheckRead(2)
	return c.data[c.read : c.read+2 : c.read+2]
}

// Write int16 to crate
func (c *Crate) WriteI16(val int16) {
	c.WriteU16(*(*uint16)(unsafe.Pointer(&val)))
}

// Read next 2 bytes from crate as int16
func (c *Crate) ReadI16() (val int16) {
	val = c.PeekI16()
	c.read += 2
	return val
}

// Read next 2 bytes from crate as int16 without advancing read index
func (c *Crate) PeekI16() (val int16) {
	uVal := c.PeekU16()
	return *(*int16)(unsafe.Pointer(&uVal))
}

// Use the int16 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI16(val *int16, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI16(*val)
	case Read:
		*val = c.ReadI16()
	case Peek:
		*val = c.PeekI16()
	case Discard:
		c.DiscardI16()
	case Slice:
		sliceModeData = c.SliceI16()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI16()")
	}
	return sliceModeData
}

/**************
	UINT24
***************/

// Discard next 3 unread bytes in crate
func (c *Crate) DiscardU24() {
	c.DiscardN(3)
}

// Return byte slice the next unread uint32 with VALUE <= 16777215 occupies
func (c *Crate) SliceU24() (slice []byte) {
	c.CheckRead(3)
	return c.data[c.read : c.read+3 : c.read+3]
}

// Write uint32 to crate as 3 bytes,
// where the value is known to always be VALUE <= 16777215
func (c *Crate) WriteU24(val uint32) {
	c.CheckWrite(3)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.data[c.write+2] = byte(val >> 16)
	c.write += 3
}

// Read next 3 bytes from crate as uint32,
// where the value is known to always be VALUE <= 16777215
func (c *Crate) ReadU24() (val uint32) {
	val = c.PeekU24()
	c.read += 3
	return val
}

// Read next 3 bytes from crate as uint32 without advancing read index,
// where the value is known to always be VALUE <= 16777215
func (c *Crate) PeekU24() (val uint32) {
	c.CheckRead(3)
	val = ( //
	/**/ uint32(c.data[c.read+0]) |
		uint32(c.data[c.read+1])<<8 |
		uint32(c.data[c.read+2])<<16)
	return val
}

// Use the uint32 (VALUE <= 16777215 as 3 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU24(val *uint32, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU24(*val)
	case Read:
		*val = c.ReadU24()
	case Peek:
		*val = c.PeekU24()
	case Discard:
		c.DiscardU24()
	case Slice:
		sliceModeData = c.SliceU24()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU24()")
	}
	return sliceModeData
}

/**************
	INT24
***************/

// Discard next 3 unread bytes in crate
func (c *Crate) DiscardI24() {
	c.DiscardN(3)
}

// Return byte slice the next unread int32 with -8388608 <= VALUE <= 8388607 occupies
func (c *Crate) SliceI24() (slice []byte) {
	c.CheckRead(3)
	return c.data[c.read : c.read+3 : c.read+3]
}

// Write int32 to crate as 3 bytes,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func (c *Crate) WriteI24(val int32) {
	val = twosComplimentShrink(val, maskI32, maskI24)
	c.WriteU24(*(*uint32)(unsafe.Pointer(&val)))
}

// Read next 3 bytes from crate as int32,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func (c *Crate) ReadI24() (val int32) {
	val = c.PeekI24()
	c.read += 3
	return val
}

// Read next 3 bytes from crate as int32 without advancing read index,
// where the value is known to always be -8388608 <= VALUE <= 8388607
func (c *Crate) PeekI24() (val int32) {
	uVal := c.PeekU24()
	val = *(*int32)(unsafe.Pointer(&uVal))
	val = twosComplimentExpand(val, minI24, maskI24, maskI32)
	return val
}

// Use the int32 (-8388608 <= VALUE <= 8388607 as 3 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI24(val *int32, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI24(*val)
	case Read:
		*val = c.ReadI24()
	case Peek:
		*val = c.PeekI24()
	case Discard:
		c.DiscardI24()
	case Slice:
		sliceModeData = c.SliceI24()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI24()")
	}
	return sliceModeData
}

/**************
	UINT32
***************/

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardU32() {
	c.DiscardN(4)
}

// Return byte slice the next unread uint32 occupies
func (c *Crate) SliceU32() (slice []byte) {
	c.CheckRead(4)
	return c.data[c.read : c.read+4 : c.read+4]
}

// Write uint32 to crate
func (c *Crate) WriteU32(val uint32) {
	c.CheckWrite(4)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.data[c.write+2] = byte(val >> 16)
	c.data[c.write+3] = byte(val >> 24)
	c.write += 4
}

// Read next 4 bytes from crate as uint32
func (c *Crate) ReadU32() (val uint32) {
	val = c.PeekU32()
	c.read += 4
	return val
}

// Read next 4 bytes from crate as uint32 without advancing read index
func (c *Crate) PeekU32() (val uint32) {
	c.CheckRead(4)
	val = ( //
	/**/ uint32(c.data[c.read+0]) |
		uint32(c.data[c.read+1])<<8 |
		uint32(c.data[c.read+2])<<16 |
		uint32(c.data[c.read+3])<<24)
	return val
}

// Use the uint32 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU32(val *uint32, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU32(*val)
	case Read:
		*val = c.ReadU32()
	case Peek:
		*val = c.PeekU32()
	case Discard:
		c.DiscardU32()
	case Slice:
		sliceModeData = c.SliceU32()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU32()")
	}
	return sliceModeData
}

/**************
	INT32/RUNE
***************/

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardI32() {
	c.DiscardN(4)
}

// Return byte slice the next unread int32 occupies
func (c *Crate) SliceI32() (slice []byte) {
	c.CheckRead(4)
	return c.data[c.read : c.read+4 : c.read+4]
}

// Write int32 to crate
func (c *Crate) WriteI32(val int32) {
	c.WriteU32(*(*uint32)(unsafe.Pointer(&val)))
}

// Read next 4 bytes from crate as int32
func (c *Crate) ReadI32() int32 {
	uVal := c.ReadU32()
	return *(*int32)(unsafe.Pointer(&uVal))
}

// Read next 4 bytes from crate as int32 without advancing read index
func (c *Crate) PeekI32() (val int32) {
	uVal := c.PeekU32()
	val = *(*int32)(unsafe.Pointer(&uVal))
	return val
}

// Use the int32 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI32(val *int32, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI32(*val)
	case Read:
		*val = c.ReadI32()
	case Peek:
		*val = c.PeekI32()
	case Discard:
		c.DiscardI32()
	case Slice:
		sliceModeData = c.SliceI32()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI32()/AccessRune()")
	}
	return sliceModeData
}

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardRune() {
	c.DiscardN(4)
}

// Return byte slice the next unread int32 occupies
func (c *Crate) SliceRune() (slice []byte) {
	c.CheckRead(4)
	return c.data[c.read : c.read+4 : c.read+4]
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
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessRune(val *rune, mode AccessMode) {
	c.AccessI32(val, mode)
}

/**************
	UINT40
***************/

// Discard next 5 unread bytes in crate
func (c *Crate) DiscardU40() {
	c.DiscardN(5)
}

// Return byte slice the next unread uint64 with VALUE <= 1099511627775 occupies
func (c *Crate) SliceU40() (slice []byte) {
	c.CheckRead(5)
	return c.data[c.read : c.read+5 : c.read+5]
}

// Write uint64 to crate as 5 bytes,
// where the value is known to always be VALUE <= 1099511627775
func (c *Crate) WriteU40(val uint64) {
	c.CheckWrite(5)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.data[c.write+2] = byte(val >> 16)
	c.data[c.write+3] = byte(val >> 24)
	c.data[c.write+4] = byte(val >> 32)
	c.write += 5
}

// Read next 5 bytes from crate as uint64,
// where the value is known to always be VALUE <= 1099511627775
func (c *Crate) ReadU40() (val uint64) {
	val = c.PeekU40()
	c.read += 5
	return val
}

// Read next 5 bytes from crate as uint64 without advancing read index,
// where the value is known to always be VALUE <= 1099511627775
func (c *Crate) PeekU40() (val uint64) {
	c.CheckRead(5)
	val = ( //
	/**/ uint64(c.data[c.read+0]) |
		uint64(c.data[c.read+1])<<8 |
		uint64(c.data[c.read+2])<<16 |
		uint64(c.data[c.read+3])<<24 |
		uint64(c.data[c.read+4])<<32)
	return val
}

// Use the uint64 (VALUE <= 1099511627775 as 5 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU40(val *uint64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU40(*val)
	case Read:
		*val = c.ReadU40()
	case Peek:
		*val = c.PeekU40()
	case Discard:
		c.DiscardU40()
	case Slice:
		sliceModeData = c.SliceU40()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU40()")
	}
	return sliceModeData
}

/**************
	INT40
***************/

// Discard next 5 unread bytes in crate
func (c *Crate) DiscardI40() {
	c.DiscardN(5)
}

// Return byte slice the next unread int64 with -549755813888 <= VALUE <= 549755813887 occupies
func (c *Crate) SliceI40() (slice []byte) {
	c.CheckRead(5)
	return c.data[c.read : c.read+5 : c.read+5]
}

// Write int64 to crate as 5 bytes,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func (c *Crate) WriteI40(val int64) {
	val = twosComplimentShrink(val, maskI64, maskI40)
	c.WriteU40(*(*uint64)(unsafe.Pointer(&val)))
}

// Read next 5 bytes from crate as int64,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func (c *Crate) ReadI40() (val int64) {
	val = c.PeekI40()
	c.read += 5
	return val
}

// Read next 5 bytes from crate as int64 without advancing read index,
// where the value is known to always be -549755813888 <= VALUE <= 549755813887
func (c *Crate) PeekI40() (val int64) {
	uVal := c.PeekU40()
	val = *(*int64)(unsafe.Pointer(&uVal))
	val = twosComplimentExpand(val, minI40, maskI40, maskI64)
	return val
}

// Use the int64 (-549755813888 <= VALUE <= 549755813887 as 5 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI40(val *int64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI40(*val)
	case Read:
		*val = c.ReadI40()
	case Peek:
		*val = c.PeekI40()
	case Discard:
		c.DiscardI40()
	case Slice:
		sliceModeData = c.SliceI40()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI40()")
	}
	return sliceModeData
}

/**************
	UINT48
***************/

// Discard next 6 unread bytes in crate
func (c *Crate) DiscardU48() {
	c.DiscardN(6)
}

// Return byte slice the next unread uint64 with VALUE <= 281474976710655 occupies
func (c *Crate) SliceU48() (slice []byte) {
	c.CheckRead(6)
	return c.data[c.read : c.read+6 : c.read+6]
}

// Write uint64 to crate as 6 bytes,
// where the value is known to always be VALUE <= 281474976710655
func (c *Crate) WriteU48(val uint64) {
	c.CheckWrite(6)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.data[c.write+2] = byte(val >> 16)
	c.data[c.write+3] = byte(val >> 24)
	c.data[c.write+4] = byte(val >> 32)
	c.data[c.write+5] = byte(val >> 40)
	c.write += 6
}

// Read next 6 bytes from crate as uint64,
// where the value is known to always be VALUE <= 281474976710655
func (c *Crate) ReadU48() (val uint64) {
	val = c.PeekU48()
	c.read += 6
	return val
}

// Read next 6 bytes from crate as uint64 without advancing read index,
// where the value is known to always be VALUE <= 281474976710655
func (c *Crate) PeekU48() (val uint64) {
	c.CheckRead(6)
	val = ( //
	/**/ uint64(c.data[c.read+0]) |
		uint64(c.data[c.read+1])<<8 |
		uint64(c.data[c.read+2])<<16 |
		uint64(c.data[c.read+3])<<24 |
		uint64(c.data[c.read+4])<<32 |
		uint64(c.data[c.read+5])<<40)
	return val
}

// Use the uint64 (VALUE <= 281474976710655 as 6 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU48(val *uint64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU48(*val)
	case Read:
		*val = c.ReadU48()
	case Peek:
		*val = c.PeekU48()
	case Discard:
		c.DiscardU48()
	case Slice:
		sliceModeData = c.SliceU48()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU48()")
	}
	return sliceModeData
}

/**************
	INT48
***************/

// Discard next 6 unread bytes in crate
func (c *Crate) DiscardI48() {
	c.DiscardN(6)
}

// Return byte slice the next unread int64 with -140737488355328 <= VALUE <= 140737488355327 occupies
func (c *Crate) SliceI48() (slice []byte) {
	c.CheckRead(6)
	return c.data[c.read : c.read+6 : c.read+6]
}

// Write int64 to crate as 6 bytes,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func (c *Crate) WriteI48(val int64) {
	val = twosComplimentShrink(val, maskI64, maskI48)
	c.WriteU48(*(*uint64)(unsafe.Pointer(&val)))
}

// Read next 6 bytes from crate as int64,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func (c *Crate) ReadI48() (val int64) {
	val = c.PeekI48()
	c.read += 6
	return val
}

// Read next 6 bytes from crate as int64 without advancing read index,
// where the value is known to always be -140737488355328 <= VALUE <= 140737488355327
func (c *Crate) PeekI48() (val int64) {
	uVal := c.PeekU48()
	val = *(*int64)(unsafe.Pointer(&uVal))
	val = twosComplimentExpand(val, minI48, maskI48, maskI64)
	return val
}

// Use the int64 (-140737488355328 <= VALUE <= 140737488355327 as 6 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI48(val *int64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI48(*val)
	case Read:
		*val = c.ReadI48()
	case Peek:
		*val = c.PeekI48()
	case Discard:
		c.DiscardI48()
	case Slice:
		sliceModeData = c.SliceI48()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI48()")
	}
	return sliceModeData
}

/**************
	UINT56
***************/

// Discard next 7 unread bytes in crate
func (c *Crate) DiscardU56() {
	c.DiscardN(7)
}

// Return byte slice the next unread uint64 with VALUE <= 72057594037927935 occupies
func (c *Crate) SliceU56() (slice []byte) {
	c.CheckRead(7)
	return c.data[c.read : c.read+7 : c.read+7]
}

// Write uint64 to crate as 7 bytes,
// where the value is known to always be VALUE <= 72057594037927935
func (c *Crate) WriteU56(val uint64) {
	c.CheckWrite(7)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.data[c.write+2] = byte(val >> 16)
	c.data[c.write+3] = byte(val >> 24)
	c.data[c.write+4] = byte(val >> 32)
	c.data[c.write+5] = byte(val >> 40)
	c.data[c.write+6] = byte(val >> 48)
	c.write += 7
}

// Read next 7 bytes from crate as uint64,
// where the value is known to always be VALUE <= 72057594037927935
func (c *Crate) ReadU56() (val uint64) {
	val = c.PeekU56()
	c.read += 7
	return val
}

// Read next 7 bytes from crate as uint64 without advancing read index,
// where the value is known to always be VALUE <= 72057594037927935
func (c *Crate) PeekU56() (val uint64) {
	c.CheckRead(7)
	val = ( //
	/**/ uint64(c.data[c.read+0]) |
		uint64(c.data[c.read+1])<<8 |
		uint64(c.data[c.read+2])<<16 |
		uint64(c.data[c.read+3])<<24 |
		uint64(c.data[c.read+4])<<32 |
		uint64(c.data[c.read+5])<<40 |
		uint64(c.data[c.read+6])<<48)
	return val
}

// Use the uint64 (VALUE <= 72057594037927935 as 7 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU56(val *uint64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU56(*val)
	case Read:
		*val = c.ReadU56()
	case Peek:
		*val = c.PeekU56()
	case Discard:
		c.DiscardU56()
	case Slice:
		sliceModeData = c.SliceU56()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU56()")
	}
	return sliceModeData
}

/**************
	INT56
***************/

// Discard next 7 unread bytes in crate
func (c *Crate) DiscardI56() {
	c.DiscardN(7)
}

// Return byte slice the next unread int64 with -36028797018963968 <= VALUE <= 36028797018963967 occupies
func (c *Crate) SliceI56() (slice []byte) {
	c.CheckRead(7)
	return c.data[c.read : c.read+7 : c.read+7]
}

// Write int64 to crate as 7 bytes,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func (c *Crate) WriteI56(val int64) {
	val = twosComplimentShrink(val, maskI64, maskI56)
	c.WriteU56(*(*uint64)(unsafe.Pointer(&val)))
}

// Read next 7 bytes from crate as int64,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func (c *Crate) ReadI56() (val int64) {
	val = c.PeekI56()
	c.read += 7
	return val
}

// Read next 7 bytes from crate as int64 without advancing read index,
// where the value is known to always be -36028797018963968 <= VALUE <= 36028797018963967
func (c *Crate) PeekI56() (val int64) {
	uVal := c.PeekU56()
	val = *(*int64)(unsafe.Pointer(&uVal))
	val = twosComplimentExpand(val, minI56, maskI56, maskI64)
	return val
}

// Use the int64 (-36028797018963968 <= VALUE <= 36028797018963967 as 7 bytes) pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI56(val *int64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI56(*val)
	case Read:
		*val = c.ReadI56()
	case Peek:
		*val = c.PeekI56()
	case Discard:
		c.DiscardI56()
	case Slice:
		sliceModeData = c.SliceI56()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI56()")
	}
	return sliceModeData
}

/**************
	UINT64
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardU64() {
	c.DiscardN(8)
}

// Return byte slice the next unread uint64 occupies
func (c *Crate) SliceU64() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write uint64 to crate
func (c *Crate) WriteU64(val uint64) {
	c.CheckWrite(8)
	c.data[c.write+0] = byte(val)
	c.data[c.write+1] = byte(val >> 8)
	c.data[c.write+2] = byte(val >> 16)
	c.data[c.write+3] = byte(val >> 24)
	c.data[c.write+4] = byte(val >> 32)
	c.data[c.write+5] = byte(val >> 40)
	c.data[c.write+6] = byte(val >> 48)
	c.data[c.write+7] = byte(val >> 56)
	c.write += 8
}

// Read next 8 bytes from crate as uint64
func (c *Crate) ReadU64() (val uint64) {
	val = c.PeekU64()
	c.read += 8
	return val
}

// Read next 8 bytes from crate as uint64 without advancing read index
func (c *Crate) PeekU64() (val uint64) {
	c.CheckRead(8)
	val = ( //
	/**/ uint64(c.data[c.read+0]) |
		uint64(c.data[c.read+1])<<8 |
		uint64(c.data[c.read+2])<<16 |
		uint64(c.data[c.read+3])<<24 |
		uint64(c.data[c.read+4])<<32 |
		uint64(c.data[c.read+5])<<40 |
		uint64(c.data[c.read+6])<<48 |
		uint64(c.data[c.read+7])<<56)
	return val
}

// Use the uint64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessU64(val *uint64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteU64(*val)
	case Read:
		*val = c.ReadU64()
	case Peek:
		*val = c.PeekU64()
	case Discard:
		c.DiscardU64()
	case Slice:
		sliceModeData = c.SliceU64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessU64()")
	}
	return sliceModeData
}

/**************
	INT64
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardI64() {
	c.DiscardN(8)
}

// Return byte slice the next unread int64 occupies
func (c *Crate) SliceI64() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write int64 to crate
func (c *Crate) WriteI64(val int64) {
	c.WriteU64(*(*uint64)(unsafe.Pointer(&val)))
}

// Read next 8 bytes from crate as int64
func (c *Crate) ReadI64() (val int64) {
	uVal := c.ReadU64()
	val = *(*int64)(unsafe.Pointer(&uVal))
	return val
}

// Read next 8 bytes from crate as int64 without advancing read index
func (c *Crate) PeekI64() (val int64) {
	uVal := c.PeekU64()
	val = *(*int64)(unsafe.Pointer(&uVal))
	return val
}

// Use the int64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessI64(val *int64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteI64(*val)
	case Read:
		*val = c.ReadI64()
	case Peek:
		*val = c.PeekI64()
	case Discard:
		c.DiscardI64()
	case Slice:
		sliceModeData = c.SliceI64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessI64()")
	}
	return sliceModeData
}

/**************
	INT
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardInt() {
	c.DiscardN(8)
}

// Return byte slice the next unread int occupies
func (c *Crate) SliceInt() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write int to crate
func (c *Crate) WriteInt(val int) {
	c.WriteI64(int64(val))
}

// Read next 8 bytes from crate as int
func (c *Crate) ReadInt() (val int) {
	val = int(c.ReadI64())
	return val
}

// Read next 8 bytes from crate as int without advancing read index
func (c *Crate) PeekInt() (val int) {
	val = int(c.PeekI64())
	return val
}

// Use the int pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessInt(val *int, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteInt(*val)
	case Read:
		*val = c.ReadInt()
	case Peek:
		*val = c.PeekInt()
	case Discard:
		c.DiscardInt()
	case Slice:
		sliceModeData = c.SliceInt()
	default:
		panic("LiteCrate: Invalid mode passed to AccessInt()")
	}
	return sliceModeData
}

/**************
	UINT
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardUint() {
	c.DiscardN(8)
}

// Return byte slice the next unread uint occupies
func (c *Crate) SliceUint() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write uint to crate
func (c *Crate) WriteUint(val uint) {
	c.WriteU64(uint64(val))
}

// Read next 8 bytes from crate as uint
func (c *Crate) ReadUint() (val uint) {
	val = uint(c.ReadU64())
	return val
}

// Read next 8 bytes from crate as uint without advancing read index
func (c *Crate) PeekUint() (val uint) {
	val = uint(c.PeekU64())
	return val
}

// Use the uint pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessUint(val *uint, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteUint(*val)
	case Read:
		*val = c.ReadUint()
	case Peek:
		*val = c.PeekUint()
	case Discard:
		c.DiscardUint()
	case Slice:
		sliceModeData = c.SliceUint()
	default:
		panic("LiteCrate: Invalid mode passed to AccessUint()")
	}
	return sliceModeData
}

/**************
	UINTPTR
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardUintPtr() {
	c.DiscardN(8)
}

// Return byte slice the next unread uintptr occupies
func (c *Crate) SliceUintPtr() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write uintptr to crate
func (c *Crate) WriteUintPtr(val uintptr) {
	c.WriteU64(uint64(val))
}

// Read next 8 bytes from crate as uintptr
func (c *Crate) ReadUintPtr() (val uintptr) {
	val = uintptr(c.ReadU64())
	return val
}

// Read next 8 bytes from crate as uintptr without advancing read index
func (c *Crate) PeekUintPtr() (val uintptr) {
	val = uintptr(c.PeekU64())
	return val
}

// Use the uintptr pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessUintPtr(val *uintptr, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteUintPtr(*val)
	case Read:
		*val = c.ReadUintPtr()
	case Peek:
		*val = c.PeekUintPtr()
	case Discard:
		c.DiscardUintPtr()
	case Slice:
		sliceModeData = c.SliceUintPtr()
	default:
		panic("LiteCrate: Invalid mode passed to AccessUintPtr()")
	}
	return sliceModeData
}

/**************
	FLOAT32
***************/

// Discard next 4 unread bytes in crate
func (c *Crate) DiscardF32() {
	c.DiscardN(4)
}

// Return byte slice the next unread float32 occupies
func (c *Crate) SliceF32() (slice []byte) {
	c.CheckRead(4)
	return c.data[c.read : c.read+4 : c.read+4]
}

// Write float32 to crate
func (c *Crate) WriteF32(val float32) {
	c.WriteU32(*(*uint32)(unsafe.Pointer(&val)))
}

// Read next 4 bytes from crate as float32
func (c *Crate) ReadF32() (val float32) {
	rVal := c.ReadU32()
	val = *(*float32)(unsafe.Pointer(&rVal))
	return val
}

// Read next 4 bytes from crate as float32 without advancing read index
func (c *Crate) PeekF32() (val float32) {
	rVal := c.PeekU32()
	val = *(*float32)(unsafe.Pointer(&rVal))
	return val
}

// Use the float32 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessF32(val *float32, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteF32(*val)
	case Read:
		*val = c.ReadF32()
	case Peek:
		*val = c.PeekF32()
	case Discard:
		c.DiscardF32()
	case Slice:
		sliceModeData = c.SliceF32()
	default:
		panic("LiteCrate: Invalid mode passed to AccessF32()")
	}
	return sliceModeData
}

/**************
	FLOAT64
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardF64() {
	c.DiscardN(8)
}

// Return byte slice the next unread float64 occupies
func (c *Crate) SliceF64() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write float64 to crate
func (c *Crate) WriteF64(val float64) {
	c.WriteU64(*(*uint64)(unsafe.Pointer(&val)))
}

// Read next 8 bytes from crate as float64
func (c *Crate) ReadF64() (val float64) {
	rVal := c.ReadU64()
	val = *(*float64)(unsafe.Pointer(&rVal))
	return val
}

// Read next 8 bytes from crate as float64 without advancing read index
func (c *Crate) PeekF64() (val float64) {
	rVal := c.PeekU64()
	val = *(*float64)(unsafe.Pointer(&rVal))
	return val
}

// Use the float64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessF64(val *float64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteF64(*val)
	case Read:
		*val = c.ReadF64()
	case Peek:
		*val = c.PeekF64()
	case Discard:
		c.DiscardF64()
	case Slice:
		sliceModeData = c.SliceF64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessF64()")
	}
	return sliceModeData
}

/**************
	COMPLEX64
***************/

// Discard next 8 unread bytes in crate
func (c *Crate) DiscardC64() {
	c.DiscardN(8)
}

// Return byte slice the next unread complex64 occupies
func (c *Crate) SliceC64() (slice []byte) {
	c.CheckRead(8)
	return c.data[c.read : c.read+8 : c.read+8]
}

// Write complex64 to crate
func (c *Crate) WriteC64(val complex64) {
	c.WriteF32(real(val))
	c.WriteF32(imag(val))
}

// Read next 8 bytes from crate as complex64
func (c *Crate) ReadC64() (val complex64) {
	r := c.ReadF32()
	i := c.ReadF32()
	val = complex(r, i)
	return val
}

// Read next 8 bytes from crate as complex64 without advancing read index
func (c *Crate) PeekC64() (val complex64) {
	idx := c.read
	val = c.ReadC64()
	c.read = idx
	return val
}

// Use the complex64 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessC64(val *complex64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteC64(*val)
	case Read:
		*val = c.ReadC64()
	case Peek:
		*val = c.PeekC64()
	case Discard:
		c.DiscardC64()
	case Slice:
		sliceModeData = c.SliceC64()
	default:
		panic("LiteCrate: Invalid mode passed to AccessC64()")
	}
	return sliceModeData
}

/**************
	COMPLEX128
***************/

// Discard next 16 unread bytes in crate
func (c *Crate) DiscardC128() {
	c.DiscardN(16)
}

// Return byte slice the next unread complex128 occupies
func (c *Crate) SliceC128() (slice []byte) {
	c.CheckRead(16)
	return c.data[c.read : c.read+16 : c.read+16]
}

// Write complex128 to crate
func (c *Crate) WriteC128(val complex128) {
	c.WriteF64(real(val))
	c.WriteF64(imag(val))
}

// Read next 16 bytes from crate as complex128
func (c *Crate) ReadC128() (val complex128) {
	r := c.ReadF64()
	i := c.ReadF64()
	val = complex(r, i)
	return val
}

// Read next 16 bytes from crate as complex128 without advancing read index
func (c *Crate) PeekC128() (val complex128) {
	idx := c.read
	val = c.ReadC128()
	c.read = idx
	return val
}

// Use the complex128 pointed to by val according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessC128(val *complex128, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteC128(*val)
	case Read:
		*val = c.ReadC128()
	case Peek:
		*val = c.PeekC128()
	case Discard:
		c.DiscardC128()
	case Slice:
		sliceModeData = c.SliceC128()
	default:
		panic("LiteCrate: Invalid mode passed to AccessC128()")
	}
	return sliceModeData
}

/**************
	UVARINT
***************/

const (
	continueMask   = 128
	countMask      = 127
	finalCountMask = 255
	countShift     = 7
	longerMask     = 18446744073709551488
)

var countMasks = [9]byte{countMask, countMask, countMask, countMask, countMask, countMask, countMask, countMask, finalCountMask}

// Discard next 1-9 unread bytes in crate,
// dependant on size of the UVarint
func (c *Crate) DiscardUVarint() (bytesDiscarded uint64) {
	n := findUVarintBytesFromData(c.data[c.read:])
	c.DiscardN(n)
	return n
}

// Return byte slice the next unread UVarint (uint64) occupies
func (c *Crate) SliceUVarint() (slice []byte) {
	n := findUVarintBytesFromData(c.data[c.read:])
	c.CheckRead(n)
	return c.data[c.read : c.read+n : c.read+n]
}

// Write uint64 to crate as msb uvarint.
// Uses 1-9 bytes dependant on size of value
func (c *Crate) WriteUVarint(val uint64) (bytesWritten uint64) {
	longer := false
	longerBit := uint8(0)
	for val > 0 || bytesWritten == 0 {
		longer = val > countMask && bytesWritten < 8
		longerBit = *(*uint8)(unsafe.Pointer(&longer)) << countShift
		c.CheckWrite(1)
		c.data[c.write] = byte(val)&countMasks[bytesWritten] | longerBit
		c.write += 1
		bytesWritten += 1
		val = val >> countShift
	}
	return bytesWritten
}

// Read next 1-9 bytes from crate as msb uvarint encoded uint64
func (c *Crate) ReadUVarint() (val uint64, bytesRead uint64) {
	longer := true
	for ; longer && bytesRead < 9; bytesRead += 1 {
		c.CheckRead(1)
		longer = c.data[c.read]&continueMask == continueMask
		val |= uint64(c.data[c.read]&countMasks[bytesRead]) << (bytesRead * countShift)
		c.read += 1
	}
	return val, bytesRead
}

// Read next 1-9 bytes from crate as msb uvarint encoded uint64
// without advancing read index
func (c *Crate) PeekUVarint() (val uint64, bytesRead uint64) {
	idx := c.read
	val, bytesRead = c.ReadUVarint()
	c.read = idx
	return val, bytesRead
}

// Use the uint64 pointed to by val as a msb uvarint according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessUVarint(val *uint64, mode AccessMode) (bytesUsed uint64, sliceModeData []byte) {
	switch mode {
	case Write:
		bytesUsed = c.WriteUVarint(*val)
	case Read:
		*val, bytesUsed = c.ReadUVarint()
	case Peek:
		*val, bytesUsed = c.PeekUVarint()
	case Discard:
		bytesUsed = c.DiscardUVarint()
	case Slice:
		sliceModeData = c.SliceUVarint()
	default:
		panic("LiteCrate: Invalid mode passed to AccessUVarint()")
	}
	return bytesUsed, sliceModeData
}

/**************
	VARINT
***************/

// Discard next 1-9 unread bytes in crate,
// dependant on size of the Varint
func (c *Crate) DiscardVarint() (bytesDiscarded uint64) {
	n := findUVarintBytesFromData(c.data[c.read:])
	c.DiscardN(n)
	return n
}

// Return byte slice the next unread Varint (int64) occupies
func (c *Crate) SliceVarint() (slice []byte) {
	n := findUVarintBytesFromData(c.data[c.read:])
	c.CheckRead(n)
	return c.data[c.read : c.read+n : c.read+n]
}

// Write int64 to crate as msb zig-zag varint.
// Uses 1-9 bytes dependant on size of value
func (c *Crate) WriteVarint(val int64) (bytesWritten uint64) {
	uVal := zigZagEncode(val)
	bytesWritten = c.WriteUVarint(uVal)
	return bytesWritten
}

// Read next 1-9 bytes from crate as msb zig-zag varint encoded int64
func (c *Crate) ReadVarint() (val int64, bytesRead uint64) {
	uVal, bytesRead := c.ReadUVarint()
	val = zigZagDecode(uVal)
	return val, bytesRead
}

// Read next 1-9 bytes from crate as msb zig-zag varint encoded int64
// without advancing read index
func (c *Crate) PeekVarint() (val int64, bytesRead uint64) {
	uVal, bytesRead := c.PeekUVarint()
	val = zigZagDecode(uVal)
	return val, bytesRead
}

// Use the int64 pointed to by val as a msb zig-zag varint according to mode:
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessVarint(val *int64, mode AccessMode) (bytesUsed uint64, sliceModeData []byte) {
	switch mode {
	case Write:
		bytesUsed = c.WriteVarint(*val)
	case Read:
		*val, bytesUsed = c.ReadVarint()
	case Peek:
		*val, bytesUsed = c.PeekVarint()
	case Discard:
		bytesUsed = c.DiscardVarint()
	case Slice:
		sliceModeData = c.SliceVarint()
	default:
		panic("LiteCrate: Invalid mode passed to AccessVarint()")
	}
	return bytesUsed, sliceModeData
}

/**************
	LENGTH-OR-NIL
***************/

// Discard next 1-9 unread bytes in crate,
// dependant on length or nil (UVarint where 0 = nil, 1 = 0, 2 = 1...)
func (c *Crate) DiscardLengthOrNil() (bytesDiscarded uint64) {
	bytesDiscarded = findUVarintBytesFromData(c.data[c.read:])
	c.DiscardN(bytesDiscarded)
	return bytesDiscarded
}

// Return byte slice the next unread length or nil occupies
// (UVarint where 0 = nil, 1 = 0, 2 = 1...)
func (c *Crate) SliceLengthOrNil() (slice []byte) {
	n := findUVarintBytesFromData(c.data[c.read:])
	c.CheckRead(n)
	return c.data[c.read : c.read+n : c.read+n]
}

// Write length or nil (UVarint where 0 = nil, 1 = 0, 2 = 1...) to crate.
// Uses 1-9 bytes dependant on length
//
// Because 0 is used to represent nil, the maximum length that can be written is
// 18446744073709551614 (WILL NOT check value for correctness)
func (c *Crate) WriteLengthOrNil(length uint64, isNil bool) (bytesWritten uint64) {
	length += 1
	if isNil {
		length = 0
	}
	bytesWritten = c.WriteUVarint(length)
	return bytesWritten
}

// Read next 1-9 bytes from crate as length or nil (UVarint where 0 = nil, 1 = 0, 2 = 1...),
func (c *Crate) ReadLengthOrNil() (length uint64, isNil bool, bytesRead uint64) {
	length, isNil, bytesRead = c.PeekLengthOrNil()
	c.read += bytesRead
	return length, isNil, bytesRead
}

// Read next 1-9 bytes from crate as length or nil (UVarint where 0 = nil, 1 = 0, 2 = 1...)
// without advancing read index
func (c *Crate) PeekLengthOrNil() (length uint64, isNil bool, bytesRead uint64) {
	length, bytesRead = c.PeekUVarint()
	isNil = length == 0
	if !isNil {
		length -= 1
	}
	return length, isNil, bytesRead
}

// Use the length pointed to and writeNil/readNil (in Write/Read mode)
// as a UVarint where 0 = nil, 1 = 0, 2 = 1..., according to mode:
// Write = 'write length or nil into crate', Read = 'read from crate into lenth and return readNil if nil',
// Peek = 'read from crate into lenth and return readNil if nil, without advancing index'
// Slice = 'Return the slice the next unread length-or-nil occupies without altering length'
func (c *Crate) AccessLengthOrNil(length *uint64, writeNil bool, mode AccessMode) (readNil bool, bytesUsed uint64, sliceModeData []byte) {
	switch mode {
	case Write:
		bytesUsed = c.WriteLengthOrNil(*length, writeNil)
	case Read:
		*length, readNil, bytesUsed = c.ReadLengthOrNil()
	case Peek:
		*length, readNil, bytesUsed = c.PeekLengthOrNil()
	case Discard:
		bytesUsed = c.DiscardLengthOrNil()
	case Slice:
		sliceModeData = c.SliceLengthOrNil()
	default:
		panic("LiteCrate: Invalid mode passed to AccessLengthOrNil()")
	}
	return readNil, bytesUsed, sliceModeData
}

/**************
	STRING
***************/

// Discard next unread string of specified length in crate
func (c *Crate) DiscardString(length uint64) {
	c.DiscardN(length)
}

// Return byte slice the next unread string of specified length occupies
func (c *Crate) SliceString(length uint64) (slice []byte) {
	c.CheckRead(length)
	return c.data[c.read : c.read+length : c.read+length]
}

// Discard next unread string with preceding length counter in crate
func (c *Crate) DiscardStringWithCounter() {
	length, _, _ := c.ReadLengthOrNil()
	c.DiscardN(length)
}

// Return byte slice the next unread string-with-length-counter occupies (not including counter)
func (c *Crate) SliceStringWithCounter() (slice []byte) {
	length, _, n := c.PeekLengthOrNil()
	return c.data[c.read+n : c.read+n+length : c.read+n+length]
}

// Write string to crate
func (c *Crate) WriteString(val string) {
	length := len64str(val)
	c.CheckWrite(length)
	bytes := make([]byte, length)
	(*sliceInternals)(unsafe.Pointer(&bytes)).data = (*stringInternals)(unsafe.Pointer(&val)).data
	copy(c.data[c.write:c.write+length], bytes)
	c.write += length
}

// Write string to crate with preceding length counter
func (c *Crate) WriteStringWithCounter(val string) {
	length := len64str(val)
	c.WriteLengthOrNil(length, false)
	c.WriteString(val)
}

// Read next string of specified byte length from crate
func (c *Crate) ReadString(length uint64) (val string) {
	if length == 0 {
		return val
	}
	c.CheckRead(length)
	bytes := make([]byte, length)
	copy(bytes, c.data[c.read:c.read+length])
	targetPtr := (*stringInternals)(unsafe.Pointer(&val))
	targetPtr.data = (*sliceInternals)(unsafe.Pointer(&bytes)).data
	targetPtr.length = len(bytes)
	c.read += length
	return val
}

// Read next string with preceding length counter from crate
func (c *Crate) ReadStringWithCounter() (val string) {
	length, _, _ := c.ReadLengthOrNil()
	val = c.ReadString(length)
	return val
}

// Read next string of specified byte length from crate without advancing read index
func (c *Crate) PeekString(length uint64) (val string) {
	idx := c.read
	val = c.ReadString(length)
	c.read = idx
	return val
}

// Read next string with preceding length counter from crate without advancing read index
func (c *Crate) PeekStringWithCounter() (val string) {
	idx := c.read
	val = c.ReadStringWithCounter()
	c.read = idx
	return val
}

// Use the string pointed to by val according to mode (with specified read length):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessString(val *string, readLength uint64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteString(*val)
	case Read:
		*val = c.ReadString(readLength)
	case Peek:
		*val = c.PeekString(readLength)
	case Discard:
		c.DiscardString(readLength)
	case Slice:
		sliceModeData = c.SliceString(readLength)
	default:
		panic("LiteCrate: Invalid mode passed to AccessString()")
	}
	return sliceModeData
}

// Use the string pointed to by val according to mode (with length counter):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessStringWithCounter(val *string, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteStringWithCounter(*val)
	case Read:
		*val = c.ReadStringWithCounter()
	case Peek:
		*val = c.PeekStringWithCounter()
	case Discard:
		c.DiscardStringWithCounter()
	case Slice:
		sliceModeData = c.SliceStringWithCounter()
	default:
		panic("LiteCrate: Invalid mode passed to AccessStringWithCounter()")
	}
	return sliceModeData
}

/**************
	[]BYTE
***************/

// Discard next unread bytes of specified length in crate
func (c *Crate) DiscardBytes(length uint64) {
	c.DiscardN(length)
}

// Return the next unread byte slice of specified length
func (c *Crate) SliceBytes(length uint64) (slice []byte) {
	c.CheckRead(length)
	return c.data[c.read : c.read+length : c.read+length]
}

// Discard next unread bytes with preceding length counter in crate
func (c *Crate) DiscardBytesWithCounter() {
	length, _, _ := c.ReadLengthOrNil()
	c.DiscardN(length)
}

// Return byte slice the next unread bytes-with-length-counter occupies (not including counter)
func (c *Crate) SliceBytesWithCounter() (slice []byte) {
	length, _, n := c.PeekLengthOrNil()
	return c.data[c.read+n : c.read+n+length : c.read+n+length]
}

// Write bytes to crate
func (c *Crate) WriteBytes(val []byte) {
	length := len64(val)
	if val == nil || length == 0 {
		return
	}
	c.CheckWrite(length)
	copy(c.data[c.write:c.write+length], val)
	c.write += length
}

// Write bytes to crate with preceding length counter
func (c *Crate) WriteBytesWithCounter(val []byte) {
	length := len64(val)
	isNil := val == nil
	c.WriteLengthOrNil(length, isNil)
	c.WriteBytes(val)
}

// Read next bytes slice of specified length from crate
func (c *Crate) ReadBytes(length uint64) (val []byte) {
	c.CheckRead(length)
	val = make([]byte, length)
	copy(val, c.data[c.read:c.read+length])
	c.read += length
	return val
}

// Read next bytes slice with preceding length counter from crate
func (c *Crate) ReadBytesWithCounter() (val []byte) {
	length, isNil, _ := c.ReadLengthOrNil()
	if isNil {
		return nil
	}
	val = c.ReadBytes(length)
	return val
}

// Read next bytes slice of specified  length from crate without advancing read index
func (c *Crate) PeekBytes(length uint64) (val []byte) {
	idx := c.read
	val = c.ReadBytes(length)
	c.read = idx
	return val
}

// Read next bytes slice with preceding length counter from crate without advancing read index
func (c *Crate) PeekBytesWithCounter() (val []byte) {
	idx := c.read
	val = c.ReadBytesWithCounter()
	c.read = idx
	return val
}

// Use the []byte pointed to by val according to mode (with specified read length):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessBytes(val *[]byte, readLength uint64, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteBytes(*val)
	case Read:
		*val = c.ReadBytes(readLength)
	case Peek:
		*val = c.PeekBytes(readLength)
	case Discard:
		c.DiscardBytes(readLength)
	case Slice:
		sliceModeData = c.SliceBytes(readLength)
	default:
		panic("LiteCrate: Invalid mode passed to AccessBytes()")
	}
	return sliceModeData
}

// Use the []byte pointed to by val according to mode (with length counter):
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessBytesWithCounter(val *[]byte, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteBytesWithCounter(*val)
	case Read:
		*val = c.ReadBytesWithCounter()
	case Peek:
		*val = c.PeekBytesWithCounter()
	case Discard:
		c.DiscardBytesWithCounter()
	case Slice:
		sliceModeData = c.SliceBytesWithCounter()
	default:
		panic("LiteCrate: Invalid mode passed to AccessBytesWithCounter()")
	}
	return sliceModeData
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

// Return byte slice the next unread SelfAccessor occupies
func (c *Crate) SliceSelfAcecessor(val SelfAccessor) (slice []byte) {
	indexBefore := c.read
	val.AccessSelf(c, Read)
	length := c.read - indexBefore
	c.read = indexBefore
	return c.data[indexBefore : indexBefore+length : indexBefore+length]
}

// Use SelfAccessor according to mode
// Write = 'write val into crate', Read = 'read from crate into val',
// Peek = 'read from crate into val without advancing index'
// Slice = 'Return the slice the next unread val occupies without altering val'
func (c *Crate) AccessSelfAccessor(val SelfAccessor, mode AccessMode) (sliceModeData []byte) {
	switch mode {
	case Write:
		c.WriteSelfAccessor(val)
	case Read:
		c.ReadSelfAccessor(val)
	case Peek:
		c.PeekSelfAccessor(val)
	case Discard:
		c.DiscardSelfAccessor(val)
	case Slice:
		sliceModeData = c.SliceSelfAcecessor(val)
	default:
		panic("LiteCrate: Invalid mode passed to AccessSelfAccessor()")
	}
	return sliceModeData
}

/**************
	SLICE/MAP
***************/

type AccessFunc[T any] func(val *T, mode AccessMode) (sliceModeData []byte)

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
func AccessSlice[T any](crate *Crate, mode AccessMode, slice *[]T, accessElementFunc AccessFunc[T]) (sliceModeData []byte) {
	length := len64(*slice)
	writeNil := *slice == nil
	readNil, _, _ := crate.AccessLengthOrNil(&length, writeNil, mode)
	switch mode {
	case Read, Peek:
		if readNil {
			*slice = nil
			return nil
		}
		if *slice == nil {
			*slice = make([]T, length)
		}
		for i := uint64(0); i < length; i += 1 {
			var elem T
			accessElementFunc(&elem, mode)
			(*slice)[i] = elem
		}
	case Write:
		if writeNil {
			return nil
		}
		for i := uint64(0); i < length; i += 1 {
			accessElementFunc(&(*slice)[i], mode)
		}
	case Slice, Discard:
		start := crate.read
		for i := uint64(0); i < length; i += 1 {
			accessElementFunc(nil, Discard)
		}
		end := crate.read
		if mode == Slice {
			crate.read = start
			return crate.data[start:end:end]
		}
	default:
		panic("LiteCrate: invalid mode passed to AccessSlice()")
	}
	return nil
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
func AccessMap[K comparable, V any](crate *Crate, mode AccessMode, Map *map[K]V, accessKeyFunc AccessFunc[K], accessValFunc AccessFunc[V]) (sliceModeData []byte) {
	mapLen := len64map(*Map)
	writeNil := *Map == nil
	readNil, _, _ := crate.AccessLengthOrNil(&mapLen, writeNil, mode)
	switch mode {
	case Read, Peek:
		if readNil {
			*Map = nil
			return nil
		}
		if *Map == nil {
			*Map = make(map[K]V, mapLen)
		}
		for i := uint64(0); i < mapLen; i += 1 {
			var key K
			var val V
			accessKeyFunc(&key, mode)
			accessValFunc(&val, mode)
			(*Map)[key] = val
		}
	case Write:
		if writeNil {
			return nil
		}
		for key, val := range *Map {
			accessKeyFunc(&key, mode)
			accessValFunc(&val, mode)
		}
	case Slice, Discard:
		start := crate.read
		for i := uint64(0); i < mapLen; i += 1 {
			accessKeyFunc(nil, Discard)
			accessValFunc(nil, Discard)
		}
		end := crate.read
		if mode == Slice {
			crate.read = start
			return crate.data[start:end:end]
		}
	default:
		panic("LiteCrate: invalid mode passed to AccessMap()")
	}
	return nil
}

/**************
	INTERNAL
***************/

func zigZagEncode(iVal int64) uint64 {
	return uint64((iVal << 1) ^ (iVal >> 63))
}

func zigZagDecode(uVal uint64) int64 {
	return int64((uVal >> 1) ^ -(uVal & 1))
}

func findUVarintBytesFromData(data []byte) uint64 {
	_ = data[len(data)-1]
	var i uint64 = 0
	longer := true
	for ; longer && i < 9; i += 1 {
		longer = data[i]&continueMask > 0
	}
	return i
}

func findUVarintBytesFromValue(value uint64) uint64 {
	switch {
	case value <= 127:
		return 1
	case value <= 16383:
		return 2
	case value <= 2097151:
		return 3
	case value <= 268435455:
		return 4
	case value <= 34359738367:
		return 5
	case value <= 4398046511103:
		return 6
	case value <= 562949953421311:
		return 7
	case value <= 72057594037927935:
		return 8
	default:
		return 9
	}
}

func findVarintBytesFromValue(value int64) uint64 {
	uVal := zigZagEncode(value)
	return findUVarintBytesFromValue(uVal)
}

type signedCompress interface {
	int32 | int64
}

func boolInt(condition bool) uint8 {
	return *(*uint8)(unsafe.Pointer(&condition))
}

func intBool(val uint8) bool {
	return *(*bool)(unsafe.Pointer(&val))
}

func twosComplimentShrink[T signedCompress](value T, largeMask T, smallMask T) T {
	if value < 0 {
		value = (((value ^ largeMask) + 1) ^ smallMask) + 1
	}
	return value
}

func twosComplimentExpand[T signedCompress](value T, minSmall T, smallMask T, largeMask T) T {
	if value&minSmall == minSmall {
		if value^minSmall == 0 {
			return (minSmall - 1) ^ largeMask
		}
		value = (((value ^ smallMask) + 1) ^ largeMask) + 1
	}
	return value
}

const (
	maskI24 int32 = 16777215          // all bits set int24
	minI24  int32 = 8388608           // most negative int24
	maskI32 int32 = -1                // all bits set int32
	maskI40 int64 = 1099511627775     // all bits set I40
	minI40  int64 = 549755813888      // most negative I40
	maskI48 int64 = 281474976710655   // all bits set I48
	minI48  int64 = 140737488355328   // most negative I48
	maskI56 int64 = 72057594037927935 // all bits set I56
	minI56  int64 = 36028797018963968 // most negative I56
	maskI64 int64 = -1                // all bits set I64
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
