package litecrate_test

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	lite "github.com/gabe-lee/litecrate"
)

var smallCrate = lite.NewCrate(64, lite.FlagManualExact)
var largeCrate = lite.NewCrate(100, lite.FlagAutoDouble)

type person struct {
	Age      uint8
	Name     string
	Mood     int64
	Phone    map[string]complex128
	Children []person
	Steps    uint32 // uint24
}

func (p *person) UseSelf(crate *lite.Crate, mode lite.UseMode) {
	crate.UseU8(&p.Age, mode)
	crate.UseStringWithCounter(&p.Name, mode)
	crate.UseI64(&p.Mood, mode)
	lite.UseMap(crate, mode, &p.Phone, crate.UseStringWithCounter, crate.UseC128)
	lite.UseSlice(crate, mode, &p.Children, func(child *person, mode lite.UseMode) []byte {
		return crate.UseSelfSerializer(child, mode)
	})
	crate.UseU24(&p.Steps, mode)
}

type jsonPerson struct {
	Age      uint8              `json:"age"`
	Name     string             `json:"name"`
	Mood     int64              `json:"mood"`
	Phone    map[string]float64 `json:"phone"` // JSON can't handle complex128
	Children []jsonPerson       `json:"children"`
	Steps    uint32             `json:"steps"`
}

var benchPerson = func() person {
	a10 := 2 + (uint(10) % 5)
	b10 := 2 + (uint(9) % 5)
	c10 := 2 + (uint(8) % 5)
	babyPhone := make(map[string]complex128, 2)
	babyPhone["Gerber"] = complex(float64(1.1111), float64(2.22222))
	babyPhone["Life"] = complex(float64(3.33333), float64(4.44444))
	baby := person{uint8(1), "Baby", int64(0), babyPhone, nil, uint32(0)}
	child1Phone := make(map[string]complex128, 2)
	child1Phone["Dad"] = complex(float64(4.1415), float64(5.23456))
	child1Phone["Mom"] = complex(float64(6.55555), float64(7.87654))
	child1Children := make([]person, 0, b10)
	child1 := person{uint8(12), "Chris", int64(-3), child1Phone, child1Children, uint32(888)}
	child2Phone := make(map[string]complex128, 2)
	child2Phone["Ughh..."] = complex(float64(2.1415), float64(10.23456))
	child2Phone["Whatever"] = complex(float64(111.55555), float64(0.87654))
	child2Children := make([]person, c10)
	child2Children[0] = baby
	child2 := person{uint8(20), "OtherChild", int64(-99), child2Phone, child2Children, uint32(777)}
	personAPhone := make(map[string]complex128, 2)
	personAPhone["Hanahanana"] = complex(float64(3.1415), float64(1.23456))
	personAPhone["Brent"] = complex(float64(5.55555), float64(9.87654))
	personAChildren := make([]person, a10)
	personAChildren[0] = child1
	personAChildren[1] = child2
	personA := person{uint8(39), "Derek", int64(-2), personAPhone, personAChildren, uint32(999)}
	return personA
}()

var benchJSONPerson = func() jsonPerson {
	a10 := 2 + (uint(10) % 5)
	b10 := 2 + (uint(9) % 5)
	c10 := 2 + (uint(8) % 5)
	babyPhone := make(map[string]float64, 2)
	babyPhone["Gerber"] = float64(1.1111)
	babyPhone["Life"] = float64(3.33333)
	baby := jsonPerson{uint8(1), "Baby", int64(0), babyPhone, nil, uint32(0)}
	child1Phone := make(map[string]float64, 2)
	child1Phone["Dad"] = float64(4.1415)
	child1Phone["Mom"] = float64(6.55555)
	child1Children := make([]jsonPerson, 0, b10)
	child1 := jsonPerson{uint8(12), "Chris", int64(-3), child1Phone, child1Children, uint32(888)}
	child2Phone := make(map[string]float64, 2)
	child2Phone["Ughh..."] = float64(2.1415)
	child2Phone["Whatever"] = float64(111.55555)
	child2Children := make([]jsonPerson, c10)
	child2Children[0] = baby
	child2 := jsonPerson{uint8(20), "OtherChild", int64(-99), child2Phone, child2Children, uint32(777)}
	personAPhone := make(map[string]float64, 2)
	personAPhone["Hanahanana"] = float64(3.1415)
	personAPhone["Brent"] = float64(5.55555)
	personAChildren := make([]jsonPerson, a10)
	personAChildren[0] = child1
	personAChildren[1] = child2
	personA := jsonPerson{uint8(39), "Derek", int64(-2), personAPhone, personAChildren, uint32(999)}
	return personA
}()

func BenchmarkSendPersonGob(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := bytes.Buffer{}
		enc := gob.NewEncoder(&buf)
		enc.Encode(benchPerson)
		dec := gob.NewDecoder(&buf)
		personB := person{}
		dec.Decode(&personB)
	}
	b.StopTimer()
	buf := bytes.Buffer{}
	enc := gob.NewEncoder(&buf)
	enc.Encode(benchPerson)
	b.ReportMetric(float64(buf.Len()), "bytes/msg")
	b.StartTimer()
}

func BenchmarkSendPersonJson(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf, _ := json.Marshal(benchJSONPerson)
		personB := person{}
		json.Unmarshal(buf, &personB)
	}
	b.StopTimer()
	buf, _ := json.Marshal(benchJSONPerson)
	b.ReportMetric(float64(len(buf)), "bytes/msg")
	b.StartTimer()
}

func BenchmarkSendPersonLiteCrate(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sendCrate := lite.NewCrate(10, lite.FlagAutoDouble)
		sendCrate.WriteSelfSerializer(&benchPerson)
		recvCrate := lite.OpenCrate(sendCrate.Data(), lite.FlagManualExact)
		personB := person{}
		recvCrate.ReadSelfSerializer(&personB)
	}
	b.StopTimer()
	sendCrate := lite.NewCrate(10, lite.FlagAutoDouble)
	sendCrate.WriteSelfSerializer(&benchPerson)
	b.ReportMetric(float64(sendCrate.Len()), "bytes/msg")
	b.StartTimer()
}

func TestVerifyComplexLayout(t *testing.T) {
	var u64a, u64b uint64 = 14279333620317718523, 13525749700575785638
	var u32a, u32b uint32 = 1182749485, 3596253468
	c64 := complex(*(*float32)(unsafe.Pointer(&u32a)), *(*float32)(unsafe.Pointer(&u32b)))
	c64r := real(c64)
	c64i := imag(c64)
	c64r2 := *(*float32)(unsafe.Pointer(&c64))
	u32a2 := *(*uint32)(unsafe.Pointer(&c64))
	c64i2 := *(*float32)(unsafe.Pointer(uintptr(unsafe.Pointer(&c64)) + 4))
	u32b2 := *(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(&c64)) + 4))
	if c64r != c64r2 {
		t.Errorf("complex64 real layout incorect, %f != %f", c64r, c64r2)
	}
	if u32a != u32a2 {
		t.Errorf("complex64 real (uint32) layout incorect, %d != %d", u32a, u32a2)
	}
	if c64i != c64i2 {
		t.Errorf("complex64 imag layout incorect, %f != %f", c64i, c64i2)
	}
	if u32b != u32b2 {
		t.Errorf("complex64 imag (uint32) layout incorect, %d != %d", u32b, u32b2)
	}
	c128 := complex(*(*float64)(unsafe.Pointer(&u64a)), *(*float64)(unsafe.Pointer(&u64b)))
	c128r := real(c128)
	c128i := imag(c128)
	c128r2 := *(*float64)(unsafe.Pointer(&c128))
	u64a2 := *(*uint64)(unsafe.Pointer(&c128))
	c128i2 := *(*float64)(unsafe.Pointer(uintptr(unsafe.Pointer(&c128)) + 8))
	u64b2 := *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(&c128)) + 8))
	if c128r != c128r2 {
		t.Errorf("complex128 real layout incorect, %f != %f", c64r, c64r2)
	}
	if u64a != u64a2 {
		t.Errorf("complex128 real (uint64) layout incorect, %d != %d", u64a, u64a2)
	}
	if c128i != c128i2 {
		t.Errorf("complex128 imag layout incorect, %f != %f", c64i, c64i2)
	}
	if u64b != u64b2 {
		t.Errorf("complex128 imag (uint64) layout incorect, %d != %d", u64b, u64b2)
	}
}

func FuzzZigZagAglorithm(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1123872834618724681))
	f.Add(int64(-8232873817281237123))
	f.Add(int64(-9223372036854775808))
	f.Add(int64(9223372036854775807))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int64) {
		b := uint64((a << 1) ^ (a >> 63))
		c := int64((b >> 1) ^ -(b & 1))
		if a != c {
			t.Errorf("ZigZag algorithm failed: %d -> %d -> %d", a, b, c)
		}
	})
}

func FuzzBool(f *testing.F) {
	f.Add(true, false)
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a bool, b bool) {
		smallCrate.Reset()
		var c, d bool
		smallCrate.UseBool(&a, lite.Write)
		smallCrate.UseBool(&b, lite.Write)
		smallCrate.UseBool(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekBool - FAIL: %v != %v", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekBool - FAIL: index was increased")
		}
		smallCrate.UseBool(nil, lite.Discard)
		if smallCrate.ReadIndex() != 1 {
			t.Error("DiscardBool - FAIL: index != 1")
		}
		if smallCrate.WriteIndex() != 2 {
			t.Error("WriteBool - FAIL: index != 2")
		}
		slice := smallCrate.UseBool(&b, lite.Slice)
		if len(slice) != 1 || cap(slice) != 1 {
			t.Error("SliceBool - FAIL: len != 1 and/or cap != 1")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadBool()
		d = recvCrate.ReadBool()
		if a != c || b != d {
			t.Errorf("Read/Write Bool - FAIL: %v != %v and/or %v != %v", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 2 {
			t.Error("ReadBool - FAIL: index != 2")
		}
	})
}

func FuzzU8(f *testing.F) {
	f.Add(uint8(10), uint8(255))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint8, b uint8) {
		smallCrate.Reset()
		var c, d uint8
		smallCrate.UseU8(&a, lite.Write)
		smallCrate.UseU8(&b, lite.Write)
		smallCrate.UseU8(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU8 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU8 - FAIL: index was increased")
		}
		smallCrate.UseU8(nil, lite.Discard)
		if smallCrate.ReadIndex() != 1 {
			t.Error("DiscardU8 - FAIL: index != 1")
		}
		if smallCrate.WriteIndex() != 2 {
			t.Error("WriteU8 - FAIL: index != 2")
		}
		slice := smallCrate.UseU8(&b, lite.Slice)
		if len(slice) != 1 || cap(slice) != 1 {
			t.Error("SliceU8 - FAIL: len != 1 and/or cap != 1")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU8()
		d = recvCrate.ReadU8()
		if a != c || b != d {
			t.Errorf("Read/Write U8 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 2 {
			t.Error("ReadU8 - FAIL: index != 2")
		}
	})
}

func FuzzI8(f *testing.F) {
	f.Add(int8(100), int8(-100))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int8, b int8) {
		smallCrate.Reset()
		var c, d int8
		smallCrate.UseI8(&a, lite.Write)
		smallCrate.UseI8(&b, lite.Write)
		smallCrate.UseI8(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI8 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI8 - FAIL: index was increased")
		}
		smallCrate.UseI8(nil, lite.Discard)
		if smallCrate.ReadIndex() != 1 {
			t.Error("DiscardI8 - FAIL: index != 1")
		}
		if smallCrate.WriteIndex() != 2 {
			t.Error("WriteI8 - FAIL: index != 2")
		}
		slice := smallCrate.UseI8(&b, lite.Slice)
		if len(slice) != 1 || cap(slice) != 1 {
			t.Error("SliceI8 - FAIL: len != 1 and/or cap != 1")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI8()
		d = recvCrate.ReadI8()
		if a != c || b != d {
			t.Errorf("Read/Write I8 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 2 {
			t.Error("ReadI8 - FAIL: index != 2")
		}
	})
}

func FuzzU16(f *testing.F) {
	f.Add(uint16(10), uint16(1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint16, b uint16) {
		smallCrate.Reset()
		var c, d uint16
		smallCrate.UseU16(&a, lite.Write)
		smallCrate.UseU16(&b, lite.Write)
		smallCrate.UseU16(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU16 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU16 - FAIL: index was increased")
		}
		smallCrate.UseU16(nil, lite.Discard)
		if smallCrate.ReadIndex() != 2 {
			t.Error("DiscardU16 - FAIL: index != 2")
		}
		if smallCrate.WriteIndex() != 4 {
			t.Error("WriteU16 - FAIL: index != 4")
		}
		slice := smallCrate.UseU16(&b, lite.Slice)
		if len(slice) != 2 || cap(slice) != 2 {
			t.Error("SliceU16 - FAIL: len != 2 and/or cap != 2")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU16()
		d = recvCrate.ReadU16()
		if a != c || b != d {
			t.Errorf("Read/Write U16 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 4 {
			t.Error("ReadU16 - FAIL: index != 4")
		}
	})
}

func FuzzI16(f *testing.F) {
	f.Add(int16(10), int16(-1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int16, b int16) {
		smallCrate.Reset()
		var c, d int16
		smallCrate.UseI16(&a, lite.Write)
		smallCrate.UseI16(&b, lite.Write)
		smallCrate.UseI16(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI16 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI16 - FAIL: index was increased")
		}
		smallCrate.UseI16(nil, lite.Discard)
		if smallCrate.ReadIndex() != 2 {
			t.Error("DiscardI16 - FAIL: index != 2")
		}
		if smallCrate.WriteIndex() != 4 {
			t.Error("WriteI16 - FAIL: index != 4")
		}
		slice := smallCrate.UseI16(&b, lite.Slice)
		if len(slice) != 2 || cap(slice) != 2 {
			t.Error("SliceI16 - FAIL: len != 2 and/or cap != 2")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI16()
		d = recvCrate.ReadI16()
		if a != c || b != d {
			t.Errorf("Read/Write I16 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 4 {
			t.Error("ReadI16 - FAIL: index != 4")
		}
	})
}

func FuzzU24(f *testing.F) {
	f.Add(uint32(10), uint32(16777215))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint32, b uint32) {
		a = a % 16777216
		b = b % 16777216
		smallCrate.Reset()
		var c, d uint32
		smallCrate.UseU24(&a, lite.Write)
		smallCrate.UseU24(&b, lite.Write)
		smallCrate.UseU24(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU24 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU24 - FAIL: index was increased")
		}
		smallCrate.UseU24(nil, lite.Discard)
		if smallCrate.ReadIndex() != 3 {
			t.Error("DiscardU24 - FAIL: index != 3")
		}
		if smallCrate.WriteIndex() != 6 {
			t.Error("WriteU24 - FAIL: index != 6")
		}
		slice := smallCrate.UseU24(&b, lite.Slice)
		if len(slice) != 3 || cap(slice) != 3 {
			t.Error("SliceU24 - FAIL: len != 3 and/or cap != 3")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU24()
		d = recvCrate.ReadU24()
		if a != c || b != d {
			t.Errorf("Read/Write U24 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 6 {
			t.Error("ReadU24 - FAIL: index != 6")
		}
	})
}

func FuzzI24(f *testing.F) {
	f.Add(int32(-8388608), int32(8388607))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int32, b int32) {
		if a < -8388608 || a > 8388607 {
			a = a % 8388608
		}
		if b < -8388608 || b > 8388607 {
			b = b % 8388608
		}
		smallCrate.Reset()
		var c, d int32
		smallCrate.UseI24(&a, lite.Write)
		smallCrate.UseI24(&b, lite.Write)
		smallCrate.UseI24(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI24 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI24 - FAIL: index was increased")
		}
		smallCrate.UseI24(nil, lite.Discard)
		if smallCrate.ReadIndex() != 3 {
			t.Error("DiscardI24 - FAIL: index != 3")
		}
		if smallCrate.WriteIndex() != 6 {
			t.Error("WriteI24 - FAIL: index != 6")
		}
		slice := smallCrate.UseI24(&b, lite.Slice)
		if len(slice) != 3 || cap(slice) != 3 {
			t.Error("SliceI24 - FAIL: len != 3 and/or cap != 3")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI24()
		d = recvCrate.ReadI24()
		if a != c || b != d {
			t.Errorf("Read/Write I24 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 6 {
			t.Error("ReadI24 - FAIL: index != 6")
		}
	})
}

func FuzzU32(f *testing.F) {
	f.Add(uint32(10), uint32(1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint32, b uint32) {
		smallCrate.Reset()
		var c, d uint32
		smallCrate.UseU32(&a, lite.Write)
		smallCrate.UseU32(&b, lite.Write)
		smallCrate.UseU32(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU32 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU32 - FAIL: index was increased")
		}
		smallCrate.UseU32(nil, lite.Discard)
		if smallCrate.ReadIndex() != 4 {
			t.Error("DiscardU32 - FAIL: index != 4")
		}
		if smallCrate.WriteIndex() != 8 {
			t.Error("WriteU32 - FAIL: index != 8")
		}
		slice := smallCrate.UseU32(&b, lite.Slice)
		if len(slice) != 4 || cap(slice) != 4 {
			t.Error("SliceU32 - FAIL: len != 4 and/or cap != 4")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU32()
		d = recvCrate.ReadU32()
		if a != c || b != d {
			t.Errorf("Read/Write U32 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 8 {
			t.Error("ReadU32 - FAIL: index != 8")
		}
	})
}

func FuzzI32(f *testing.F) {
	f.Add(int32(10), int32(-100000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int32, b int32) {
		smallCrate.Reset()
		var c, d int32
		smallCrate.UseI32(&a, lite.Write)
		smallCrate.UseI32(&b, lite.Write)
		smallCrate.UseI32(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI32 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI32 - FAIL: index was increased")
		}
		smallCrate.UseI32(nil, lite.Discard)
		if smallCrate.ReadIndex() != 4 {
			t.Error("DiscardI32 - FAIL: index != 4")
		}
		if smallCrate.WriteIndex() != 8 {
			t.Error("WriteI32 - FAIL: index != 8")
		}
		slice := smallCrate.UseI32(&b, lite.Slice)
		if len(slice) != 4 || cap(slice) != 4 {
			t.Error("SliceI32 - FAIL: len != 4 and/or cap != 4")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI32()
		d = recvCrate.ReadI32()
		if a != c || b != d {
			t.Errorf("Read/Write I32 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 8 {
			t.Error("ReadI32 - FAIL: index != 8")
		}
	})
}

func FuzzU40(f *testing.F) {
	f.Add(uint64(10), uint64(1099511627775))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint64, b uint64) {
		a = a % 1099511627776
		b = b % 1099511627776
		smallCrate.Reset()
		var c, d uint64
		smallCrate.UseU40(&a, lite.Write)
		smallCrate.UseU40(&b, lite.Write)
		smallCrate.UseU40(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU40 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU40 - FAIL: index was increased")
		}
		smallCrate.UseU40(nil, lite.Discard)
		if smallCrate.ReadIndex() != 5 {
			t.Error("DiscardU40 - FAIL: index != 5")
		}
		if smallCrate.WriteIndex() != 10 {
			t.Error("WriteU40 - FAIL: index != 10")
		}
		slice := smallCrate.UseU40(&b, lite.Slice)
		if len(slice) != 5 || cap(slice) != 5 {
			t.Error("SliceU40 - FAIL: len != 5 and/or cap != 5")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU40()
		d = recvCrate.ReadU40()
		if a != c || b != d {
			t.Errorf("Read/Write U40 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 10 {
			t.Error("ReadU40 - FAIL: index != 10")
		}
	})
}

func FuzzI40(f *testing.F) {
	f.Add(int64(-549755813888), int64(549755813887))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		if a < -549755813888 || a > 549755813887 {
			a = a % 549755813888
		}
		if b < -549755813888 || b > 549755813887 {
			b = b % 549755813888
		}
		smallCrate.Reset()
		var c, d int64
		smallCrate.UseI40(&a, lite.Write)
		smallCrate.UseI40(&b, lite.Write)
		smallCrate.UseI40(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI40 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI40 - FAIL: index was increased")
		}
		smallCrate.UseI40(nil, lite.Discard)
		if smallCrate.ReadIndex() != 5 {
			t.Error("DiscardI40 - FAIL: index != 5")
		}
		if smallCrate.WriteIndex() != 10 {
			t.Error("WriteI40 - FAIL: index != 10")
		}
		slice := smallCrate.UseI40(&b, lite.Slice)
		if len(slice) != 5 || cap(slice) != 5 {
			t.Error("SliceI40 - FAIL: len != 5 and/or cap != 5")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI40()
		d = recvCrate.ReadI40()
		if a != c || b != d {
			t.Errorf("Read/Write I40 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 10 {
			t.Error("ReadI40 - FAIL: index != 10")
		}
	})
}

func FuzzU48(f *testing.F) {
	f.Add(uint64(10), uint64(281474976710655))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint64, b uint64) {
		a = a % 281474976710656
		b = b % 281474976710656
		smallCrate.Reset()
		var c, d uint64
		smallCrate.UseU48(&a, lite.Write)
		smallCrate.UseU48(&b, lite.Write)
		smallCrate.UseU48(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU48 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU48 - FAIL: index was increased")
		}
		smallCrate.UseU48(nil, lite.Discard)
		if smallCrate.ReadIndex() != 6 {
			t.Error("DiscardU48 - FAIL: index != 6")
		}
		if smallCrate.WriteIndex() != 12 {
			t.Error("WriteU48 - FAIL: index != 12")
		}
		slice := smallCrate.UseU48(&b, lite.Slice)
		if len(slice) != 6 || cap(slice) != 6 {
			t.Error("SliceU48 - FAIL: len != 6 and/or cap != 6")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU48()
		d = recvCrate.ReadU48()
		if a != c || b != d {
			t.Errorf("Read/Write U48 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 12 {
			t.Error("ReadU48 - FAIL: index != 12")
		}
	})
}

func FuzzI48(f *testing.F) {
	f.Add(int64(-140737488355328), int64(140737488355327))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		if a < -140737488355328 || a > 140737488355327 {
			a = a % 140737488355328
		}
		if b < -140737488355328 || b > 140737488355327 {
			b = b % 140737488355328
		}
		smallCrate.Reset()
		var c, d int64
		smallCrate.UseI48(&a, lite.Write)
		smallCrate.UseI48(&b, lite.Write)
		smallCrate.UseI48(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI48 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI48 - FAIL: index was increased")
		}
		smallCrate.UseI48(nil, lite.Discard)
		if smallCrate.ReadIndex() != 6 {
			t.Error("DiscardI48 - FAIL: index != 6")
		}
		if smallCrate.WriteIndex() != 12 {
			t.Error("WriteI48 - FAIL: index != 12")
		}
		slice := smallCrate.UseI48(&b, lite.Slice)
		if len(slice) != 6 || cap(slice) != 6 {
			t.Error("SliceI48 - FAIL: len != 6 and/or cap != 6")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI48()
		d = recvCrate.ReadI48()
		if a != c || b != d {
			t.Errorf("Read/Write I48 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 12 {
			t.Error("ReadI48 - FAIL: index != 12")
		}
	})
}

func FuzzU56(f *testing.F) {
	f.Add(uint64(10), uint64(72057594037927935))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint64, b uint64) {
		a = a % 72057594037927936
		b = b % 72057594037927936
		smallCrate.Reset()
		var c, d uint64
		smallCrate.UseU56(&a, lite.Write)
		smallCrate.UseU56(&b, lite.Write)
		smallCrate.UseU56(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU56 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU56 - FAIL: index was increased")
		}
		smallCrate.UseU56(nil, lite.Discard)
		if smallCrate.ReadIndex() != 7 {
			t.Error("DiscardU56 - FAIL: index != 7")
		}
		if smallCrate.WriteIndex() != 14 {
			t.Error("WriteU56 - FAIL: index != 14")
		}
		slice := smallCrate.UseU56(&b, lite.Slice)
		if len(slice) != 7 || cap(slice) != 7 {
			t.Error("SliceU56 - FAIL: len != 7 and/or cap != 7")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU56()
		d = recvCrate.ReadU56()
		if a != c || b != d {
			t.Errorf("Read/Write U56 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 14 {
			t.Error("ReadU56 - FAIL: index != 14")
		}
	})
}

func FuzzI56(f *testing.F) {
	f.Add(int64(-36028797018963968), int64(36028797018963967))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		if a < -36028797018963968 || a > 36028797018963967 {
			a = a % 36028797018963968
		}
		if b < -36028797018963968 || b > 36028797018963967 {
			b = b % 36028797018963968
		}
		smallCrate.Reset()
		var c, d int64
		smallCrate.UseI56(&a, lite.Write)
		smallCrate.UseI56(&b, lite.Write)
		smallCrate.UseI56(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI56 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI56 - FAIL: index was increased")
		}
		smallCrate.UseI56(nil, lite.Discard)
		if smallCrate.ReadIndex() != 7 {
			t.Error("DiscardI56 - FAIL: index != 7")
		}
		if smallCrate.WriteIndex() != 14 {
			t.Error("WriteI56 - FAIL: index != 14")
		}
		slice := smallCrate.UseI56(&b, lite.Slice)
		if len(slice) != 7 || cap(slice) != 7 {
			t.Error("SliceI56 - FAIL: len != 7 and/or cap != 7")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI56()
		d = recvCrate.ReadI56()
		if a != c || b != d {
			t.Errorf("Read/Write I56 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 14 {
			t.Error("ReadI56 - FAIL: index != 14")
		}
	})
}

func FuzzU64(f *testing.F) {
	f.Add(uint64(10), uint64(1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint64, b uint64) {
		smallCrate.Reset()
		var c, d uint64
		smallCrate.UseU64(&a, lite.Write)
		smallCrate.UseU64(&b, lite.Write)
		smallCrate.UseU64(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekU64 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekU64 - FAIL: index was increased")
		}
		smallCrate.UseU64(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardU64 - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteU64 - FAIL: index != 16")
		}
		slice := smallCrate.UseU64(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceU64 - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadU64()
		d = recvCrate.ReadU64()
		if a != c || b != d {
			t.Errorf("Read/Write U64 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadU64 - FAIL: index != 16")
		}
	})
}

func FuzzI64(f *testing.F) {
	f.Add(int64(10), int64(-1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		smallCrate.Reset()
		var c, d int64
		smallCrate.UseI64(&a, lite.Write)
		smallCrate.UseI64(&b, lite.Write)
		smallCrate.UseI64(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekI64 - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekI64 - FAIL: index was increased")
		}
		smallCrate.UseI64(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardI64 - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteI64 - FAIL: index != 16")
		}
		slice := smallCrate.UseI64(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceI64 - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadI64()
		d = recvCrate.ReadI64()
		if a != c || b != d {
			t.Errorf("Read/Write I64 - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadI64 - FAIL: index != 16")
		}
	})
}

func FuzzInt(f *testing.F) {
	f.Add(int(10), int(-1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int, b int) {
		smallCrate.Reset()
		var c, d int
		smallCrate.UseInt(&a, lite.Write)
		smallCrate.UseInt(&b, lite.Write)
		smallCrate.UseInt(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekInt - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekInt - FAIL: index was increased")
		}
		smallCrate.UseInt(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardInt - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteInt - FAIL: index != 16")
		}
		slice := smallCrate.UseInt(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceInt - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadInt()
		d = recvCrate.ReadInt()
		if a != c || b != d {
			t.Errorf("Read/Write Int - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadInt - FAIL: index != 16")
		}
	})
}

func FuzzUint(f *testing.F) {
	f.Add(uint(10), uint(1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint, b uint) {
		smallCrate.Reset()
		var c, d uint
		smallCrate.UseUint(&a, lite.Write)
		smallCrate.UseUint(&b, lite.Write)
		smallCrate.UseUint(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekUint - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekUint - FAIL: index was increased")
		}
		smallCrate.UseUint(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardUint - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteUint - FAIL: index != 16")
		}
		slice := smallCrate.UseUint(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceUint - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadUint()
		d = recvCrate.ReadUint()
		if a != c || b != d {
			t.Errorf("Read/Write Uint - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadUint - FAIL: index != 16")
		}
	})
}

func FuzzUINTPtr(f *testing.F) {
	f.Add(uint(10), uint(1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, aa uint, bb uint) {
		smallCrate.Reset()
		var a, b uintptr = uintptr(aa), uintptr(bb)
		var c, d uintptr
		smallCrate.UseUintPtr(&a, lite.Write)
		smallCrate.UseUintPtr(&b, lite.Write)
		smallCrate.UseUintPtr(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekUintPtr - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekUintPtr - FAIL: index was increased")
		}
		smallCrate.UseUintPtr(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardUintPtr - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteUintPtr - FAIL: index != 16")
		}
		slice := smallCrate.UseUintPtr(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceUintPtr - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadUintPtr()
		d = recvCrate.ReadUintPtr()
		if a != c || b != d {
			t.Errorf("Read/Write UintPtr - FAIL: %d != %d and/or %d != %d", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadUintPtr - FAIL: index != 16")
		}
	})
}

func FuzzF32(f *testing.F) {
	f.Add(float32(10), float32(-1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a float32, b float32) {
		smallCrate.Reset()
		var c, d float32
		smallCrate.UseF32(&a, lite.Write)
		smallCrate.UseF32(&b, lite.Write)
		smallCrate.UseF32(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekF32 - FAIL: %f != %f", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekF32 - FAIL: index was increased")
		}
		smallCrate.UseF32(nil, lite.Discard)
		if smallCrate.ReadIndex() != 4 {
			t.Error("DiscardF32 - FAIL: index != 4")
		}
		if smallCrate.WriteIndex() != 8 {
			t.Error("WriteF32 - FAIL: index != 8")
		}
		slice := smallCrate.UseF32(&b, lite.Slice)
		if len(slice) != 4 || cap(slice) != 4 {
			t.Error("SliceF32 - FAIL: len != 4 and/or cap != 4")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadF32()
		d = recvCrate.ReadF32()
		if a != c || b != d {
			t.Errorf("Read/Write F32 - FAIL: %f != %f and/or %f != %f", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 8 {
			t.Error("ReadF32 - FAIL: index != 8")
		}
	})
}

func FuzzF64(f *testing.F) {
	f.Add(float64(10), float64(-1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a float64, b float64) {
		smallCrate.Reset()
		var c, d float64
		smallCrate.UseF64(&a, lite.Write)
		smallCrate.UseF64(&b, lite.Write)
		smallCrate.UseF64(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekF64 - FAIL: %f != %f", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekF64 - FAIL: index was increased")
		}
		smallCrate.UseF64(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardF64 - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteF64 - FAIL: index != 16")
		}
		slice := smallCrate.UseF64(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceF64 - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadF64()
		d = recvCrate.ReadF64()
		if a != c || b != d {
			t.Errorf("Read/Write F64 - FAIL: %f != %f and/or %f != %f", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadF64 - FAIL: index != 16")
		}
	})
}

func FuzzC64(f *testing.F) {
	f.Add(float32(10), float32(-1000), float32(11), float32(-1001))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, ar float32, br float32, ai float32, bi float32) {
		smallCrate.Reset()
		a, b := complex(ar, ai), complex(br, bi)
		var c, d complex64
		smallCrate.UseC64(&a, lite.Write)
		smallCrate.UseC64(&b, lite.Write)
		smallCrate.UseC64(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekC64 - FAIL: %f != %f", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekC64 - FAIL: index was increased")
		}
		smallCrate.UseC64(nil, lite.Discard)
		if smallCrate.ReadIndex() != 8 {
			t.Error("DiscardC64 - FAIL: index != 8")
		}
		if smallCrate.WriteIndex() != 16 {
			t.Error("WriteC64 - FAIL: index != 16")
		}
		slice := smallCrate.UseC64(&b, lite.Slice)
		if len(slice) != 8 || cap(slice) != 8 {
			t.Error("SliceC64 - FAIL: len != 8 and/or cap != 8")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadC64()
		d = recvCrate.ReadC64()
		if a != c || b != d {
			t.Errorf("Read/Write C64 - FAIL: %f != %f and/or %f != %f", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 16 {
			t.Error("ReadC64 - FAIL: index != 16")
		}
	})
}

func FuzzC128(f *testing.F) {
	f.Add(float64(10), float64(-1000), float64(11), float64(-1001))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, ar float64, br float64, ai float64, bi float64) {
		smallCrate.Reset()
		a, b := complex(ar, ai), complex(br, bi)
		var c, d complex128
		smallCrate.UseC128(&a, lite.Write)
		smallCrate.UseC128(&b, lite.Write)
		smallCrate.UseC128(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekC128 - FAIL: %f != %f", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekC128 - FAIL: index was increased")
		}
		smallCrate.UseC128(nil, lite.Discard)
		if smallCrate.ReadIndex() != 16 {
			t.Error("DiscardC128 - FAIL: index != 16")
		}
		if smallCrate.WriteIndex() != 32 {
			t.Error("WriteC128 - FAIL: index != 32")
		}
		slice := smallCrate.UseC128(&b, lite.Slice)
		if len(slice) != 16 || cap(slice) != 16 {
			t.Error("SliceC128 - FAIL: len != 16 and/or cap != 16")
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadC128()
		d = recvCrate.ReadC128()
		if a != c || b != d {
			t.Errorf("Read/Write C128 - FAIL: %f != %f and/or %f != %f", a, c, b, d)
		}
		if recvCrate.ReadIndex() != 32 {
			t.Error("ReadC128 - FAIL: index != 32")
		}
	})
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

func zigZagEncode(iVal int64) uint64 {
	return uint64((iVal << 1) ^ (iVal >> 63))
}

func findVarintBytesFromValue(value int64) uint64 {
	uVal := zigZagEncode(value)
	return findUVarintBytesFromValue(uVal)
}

func FuzzUVarint(f *testing.F) {
	f.Add(uint64(10), uint64(1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint64, b uint64) {
		smallCrate.Reset()
		var c, d, cBytes, dBytes uint64
		bytesA, bytesB := findUVarintBytesFromValue(a), findUVarintBytesFromValue(b)
		bytesTotal := bytesA + bytesB
		smallCrate.UseUVarint(&a, lite.Write)
		smallCrate.UseUVarint(&b, lite.Write)
		smallCrate.UseUVarint(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekUVarint - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekUVarint - FAIL: index was increased")
		}
		smallCrate.UseUVarint(nil, lite.Discard)
		if smallCrate.ReadIndex() != bytesA {
			t.Errorf("DiscardUVarint - FAIL: index %d != %d", smallCrate.ReadIndex(), bytesA)
		}
		if smallCrate.WriteIndex() != bytesTotal {
			t.Errorf("WriteUVarint - FAIL: index %d != %d", smallCrate.WriteIndex(), bytesTotal)
		}
		_, slice := smallCrate.UseUVarint(&b, lite.Slice)
		if uint64(len(slice)) != bytesB || uint64(cap(slice)) != bytesB {
			t.Error("SliceUVarint - FAIL: len(", len(slice), ") != ", bytesB, " and/or cap(", cap(slice), ") != ", bytesB)
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c, cBytes = recvCrate.ReadUVarint()
		d, dBytes = recvCrate.ReadUVarint()
		if a != c || b != d {
			t.Errorf("Read/Write UVarint - FAIL (value): %d != %d and/or %d != %d", a, c, b, d)
		}
		if bytesA != cBytes || bytesB != dBytes {
			t.Errorf("Read/Write UVarint - FAIL (bytes): %d != %d and/or %d != %d ", bytesA, cBytes, bytesB, dBytes)
		}
		if recvCrate.ReadIndex() != bytesTotal {
			t.Error("ReadUVarint - FAIL: index != ", bytesTotal)
		}
	})
}

func FuzzVarint(f *testing.F) {
	f.Add(int64(10), int64(-1000))
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		smallCrate.Reset()
		var c, d int64
		var cBytes, dBytes uint64
		bytesA, bytesB := findVarintBytesFromValue(a), findVarintBytesFromValue(b)
		bytesTotal := bytesA + bytesB
		smallCrate.UseVarint(&a, lite.Write)
		smallCrate.UseVarint(&b, lite.Write)
		smallCrate.UseVarint(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekVarint - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekVarint - FAIL: index was increased")
		}
		smallCrate.UseVarint(nil, lite.Discard)
		if smallCrate.ReadIndex() != bytesA {
			t.Error("DiscardVarint - FAIL: index != ", bytesA)
		}
		if smallCrate.WriteIndex() != bytesTotal {
			t.Error("WriteVarint - FAIL: index != ", bytesTotal)
		}
		_, slice := smallCrate.UseVarint(&b, lite.Slice)
		if uint64(len(slice)) != bytesB || uint64(cap(slice)) != bytesB {
			t.Error("SliceVarint - FAIL: len != ", bytesB, " and/or cap != ", bytesB)
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c, cBytes = recvCrate.ReadVarint()
		d, dBytes = recvCrate.ReadVarint()
		if a != c || b != d {
			t.Errorf("Read/Write Varint - FAIL (value): %d != %d and/or %d != %d", a, c, b, d)
		}
		if bytesA != cBytes || bytesB != dBytes {
			t.Errorf("Read/Write Varint - FAIL (bytes): %d != %d and/or %d != %d ", bytesA, cBytes, bytesB, dBytes)
		}
		if recvCrate.ReadIndex() != bytesTotal {
			t.Error("ReadVarint - FAIL: index != ", bytesTotal)
		}
	})
}

func FuzzLengthOrNil(f *testing.F) {
	f.Add(uint64(10), uint64(1000), false, false)
	smallCrate.FullClear()
	f.Fuzz(func(t *testing.T, a uint64, b uint64, aNil bool, bNil bool) {
		smallCrate.Reset()
		var n uint64 = 0
		nNil := true
		var c, d, e, cBytes, dBytes, eBytes uint64
		var cNil, dNil, eNil bool
		a = (a % 18446744073709551615)
		b = (b % 18446744073709551615)
		if aNil {
			a = 0
		}
		if bNil {
			b = 0
		}
		bytesA, bytesB, bytesN := findUVarintBytesFromValue(a+1), findUVarintBytesFromValue(b+1), findUVarintBytesFromValue(n)
		bytesTotal := bytesA + bytesB + bytesN
		smallCrate.UseLengthOrNil(&a, aNil, lite.Write)
		smallCrate.UseLengthOrNil(&b, bNil, lite.Write)
		smallCrate.UseLengthOrNil(&n, nNil, lite.Write)
		smallCrate.UseLengthOrNil(&c, cNil, lite.Peek)
		if c != a {
			t.Errorf("PeekLengthOrNil - FAIL: %d != %d", c, a)
		}
		if smallCrate.ReadIndex() != 0 {
			t.Error("PeekLengthOrNil - FAIL: index was increased")
		}
		smallCrate.UseLengthOrNil(nil, false, lite.Discard)
		if smallCrate.ReadIndex() != bytesA {
			t.Error("DiscardLengthOrNil - FAIL: index != ", bytesA)
		}
		if smallCrate.WriteIndex() != bytesTotal {
			t.Error("WriteLengthOrNil - FAIL: index != ", bytesTotal)
		}
		_, _, slice := smallCrate.UseLengthOrNil(&b, false, lite.Slice)
		if uint64(len(slice)) != bytesB || uint64(cap(slice)) != bytesB {
			t.Error("SliceLengthOrNil - FAIL: len != ", bytesB, " and/or cap != ", bytesB)
		}
		recvCrate := lite.OpenCrate(smallCrate.Data(), lite.FlagManualExact)
		c, cNil, cBytes = recvCrate.ReadLengthOrNil()
		d, dNil, dBytes = recvCrate.ReadLengthOrNil()
		e, eNil, eBytes = recvCrate.ReadLengthOrNil()
		if a != c || b != d || n != e {
			t.Errorf("Read/Write LengthOrNil - FAIL (value): %d != %d and/or %d != %d and/or %d != %d", a, c, b, d, n, e)
		}
		if aNil != cNil || bNil != dNil || nNil != eNil {
			t.Errorf("Read/Write LengthOrNil - FAIL (nility): %v != %v and/or %v != %v and/or %v != %v", aNil, cNil, bNil, dNil, nNil, eNil)
		}
		if bytesA != cBytes || bytesB != dBytes || bytesN != eBytes {
			t.Errorf("Read/Write LengthOrNil - FAIL (bytes): %d != %d and/or %d != %d and/or %d != %d", bytesA, cBytes, bytesB, dBytes, bytesN, eBytes)
		}
		if recvCrate.ReadIndex() != bytesTotal {
			t.Error("ReadLengthOrNil - FAIL: index != ", bytesTotal)
		}
	})
}

func FuzzString(f *testing.F) {
	f.Add("HelloWorld", "FooBar")
	largeCrate.FullClear()
	f.Fuzz(func(t *testing.T, a string, b string) {
		largeCrate.Reset()
		var c, d string
		largeCrate.UseStringWithCounter(&a, lite.Write)
		largeCrate.UseStringWithCounter(&b, lite.Write)
		largeCrate.UseStringWithCounter(&c, lite.Peek)
		if c != a {
			t.Errorf("PeekString - FAIL: %s != %s", c, a)
		}
		if largeCrate.ReadIndex() != 0 {
			t.Error("PeekString - FAIL: index was increased")
		}
		slice := largeCrate.UseStringWithCounter(&a, lite.Slice)
		if len(slice) != len(a) || cap(slice) != len(a) {
			t.Errorf("SliceStringWithCounter - FAIL: len(%d) != %d and/or cap(%d) != %d", len(slice), len(a), cap(slice), len(a))
		}
		recvCrate := lite.OpenCrate(largeCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadStringWithCounter()
		d = recvCrate.ReadStringWithCounter()
		if a != c || b != d {
			t.Errorf("Read/Write String - FAIL: \n%s != \n%s \nand/or \n%s != \n%s", a, c, b, d)
		}
	})
}

func FuzzBytes(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5}, []byte{6, 7, 8, 9, 10, 11, 12, 13})
	largeCrate.FullClear()
	f.Fuzz(func(t *testing.T, a []byte, b []byte) {
		largeCrate.Reset()
		var c, d []byte
		largeCrate.UseBytesWithCounter(&a, lite.Write)
		largeCrate.UseBytesWithCounter(&b, lite.Write)
		largeCrate.UseBytesWithCounter(&c, lite.Peek)
		for i := 0; i < len(c) && i < len(a); i += 1 {
			if len(c) != len(a) || c[i] != a[i] {
				t.Errorf("PeekBytes - FAIL: \n%v != \n%v", c, a)
				break
			}
		}
		if largeCrate.ReadIndex() != 0 {
			t.Error("PeekBytes - FAIL: index was increased")
		}
		slice := largeCrate.UseBytesWithCounter(&a, lite.Slice)
		if len(slice) != len(a) || cap(slice) != len(a) {
			t.Errorf("SliceBytesWithCounter - FAIL: len(%d) != %d and/or cap(%d) != %d", len(slice), len(a), cap(slice), len(a))
		}
		recvCrate := lite.OpenCrate(largeCrate.Data(), lite.FlagManualExact)
		c = recvCrate.ReadBytesWithCounter()
		d = recvCrate.ReadBytesWithCounter()
		for i := 0; i < len(c) && i < len(a); i += 1 {
			if len(c) != len(a) || c[i] != a[i] {
				t.Errorf("Read/Write Bytes - FAIL: \n%v != \n%v \nand/or \n%v != \n%v", a, c, b, d)
				break
			}
		}
		for i := 0; i < len(d) && i < len(b); i += 1 {
			if len(d) != len(b) || d[i] != b[i] {
				t.Errorf("Read/Write Bytes - FAIL: \n%v != \n%v \nand/or \n%v != \n%v", a, c, b, d)
				break
			}
		}
	})
}

func FuzzSelfSerializer(f *testing.F) {
	f.Add(
		uint8(38), "Derek", int64(-2), "Hanahanana", float64(3.1415), float64(1.23456), "Brent", float64(5.55555), float64(9.87654), uint(10), uint32(999),
		uint8(12), "Chris", int64(-3), "Dad", float64(4.1415), float64(5.23456), "Mom", float64(6.55555), float64(7.87654), uint(9), uint32(888),
		uint8(20), "OtherChild", int64(-99), "Ughh...", float64(2.1415), float64(10.23456), "Whatever", float64(111.55555), float64(0.87654), uint(8), uint32(777),
		uint8(1), "Baby", int64(0), "Gerber", float64(1.1111), float64(2.22222), "Life", float64(3.33333), float64(4.44444), uint(0), uint32(0),
	)
	f.Fuzz(func(
		t *testing.T,
		a1 uint8, a2 string, a3 int64, a4 string, a5 float64, a6 float64, a7 string, a8 float64, a9 float64, a10 uint, a11 uint32,
		b1 uint8, b2 string, b3 int64, b4 string, b5 float64, b6 float64, b7 string, b8 float64, b9 float64, b10 uint, b11 uint32,
		c1 uint8, c2 string, c3 int64, c4 string, c5 float64, c6 float64, c7 string, c8 float64, c9 float64, c10 uint, c11 uint32,
		d1 uint8, d2 string, d3 int64, d4 string, d5 float64, d6 float64, d7 string, d8 float64, d9 float64, d10 uint, d11 uint32,
	) {
		// init
		largeCrate.Reset()
		a10 = 2 + (a10 % 5)
		b10 = 2 + (b10 % 5)
		c10 = 2 + (c10 % 5)
		d10 = 2 + (d10 % 5)
		babyPhone := make(map[string]complex128, 2)
		babyPhone[d4] = complex(d5, d6)
		babyPhone[d7] = complex(d8, d9)
		baby := person{d1, d2, d3, babyPhone, nil, d11}
		child1Phone := make(map[string]complex128, 2)
		child1Phone[b4] = complex(b5, b6)
		child1Phone[b7] = complex(b8, b9)
		child1Children := make([]person, 0, b10)
		child1 := person{b1, b2, b3, child1Phone, child1Children, b11}
		child2Phone := make(map[string]complex128, 2)
		child2Phone[c4] = complex(c5, c6)
		child2Phone[c7] = complex(c8, c9)
		child2Children := make([]person, c10)
		child2Children[0] = baby
		child2 := person{c1, c2, c3, child2Phone, child2Children, c11}
		personAPhone := make(map[string]complex128, 2)
		personAPhone[a4] = complex(a5, a6)
		personAPhone[a7] = complex(a8, a9)
		personAChildren := make([]person, a10)
		personAChildren[0] = child1
		personAChildren[1] = child2
		personA := person{a1, a2, a3, personAPhone, personAChildren, a11}
		personB := person{}
		personC := person{}
		// read/write
		largeCrate.UseSelfSerializer(&personA, lite.Write)
		largeCrate.UseSelfSerializer(&personB, lite.Peek)
		if largeCrate.ReadIndex() != 0 {
			t.Error("PeekSelfSelector - FAIL: index was increased")
		}
		if !reflect.DeepEqual(personA, personB) {
			outputA := fmt.Sprintf("%#v", personA)
			outputB := fmt.Sprintf("%#v", personB)
			t.Errorf("PeekSelfSelector - FAIL: \n%#v != \n%#v", personA, personB)
			t.Logf("Verbose Strings Equal? %v", outputA == outputB)
		}
		recvCrate := lite.OpenCrate(largeCrate.Data(), lite.FlagManualExact)
		recvCrate.UseSelfSerializer(&personC, lite.Read)
		if !reflect.DeepEqual(personA, personC) {
			outputA := fmt.Sprintf("%#v", personA)
			outputC := fmt.Sprintf("%#v", personC)
			t.Errorf("Read/Write SelfSelector - FAIL: \n%#v != \n%#v", personA, personC)
			t.Logf("Verbose Strings Equal? %v", outputA == outputC)
		}
	})
}
