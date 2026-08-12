package proto

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	Varint          = 0
	Fixed64         = 1
	LengthDelimited = 2
	Fixed32         = 5
)

type Encoder struct {
	buf []byte
}

func NewEncoder() *Encoder {
	return &Encoder{buf: make([]byte, 0, 1024)}
}

func (e *Encoder) Bytes() []byte { return e.buf }

func (e *Encoder) Reset() { e.buf = e.buf[:0] }

func (e *Encoder) tag(fieldNum uint32, wireType int) {
	e.varint(uint64(fieldNum)<<3 | uint64(wireType))
}

func (e *Encoder) varint(v uint64) {
	b := make([]byte, 0, 10)
	for {
		b = append(b, byte(v)&0x7f|0x80)
		v >>= 7
		if v == 0 {
			b[len(b)-1] &= 0x7f
			break
		}
	}
	e.buf = append(e.buf, b...)
}

func (e *Encoder) Int32(fieldNum uint32, v int32) {
	e.tag(fieldNum, Varint)
	e.varint(uint64(v))
}

func (e *Encoder) Uint32Field(fieldNum uint32, v uint32) {
	e.tag(fieldNum, Varint)
	e.varint(uint64(v))
}

func (e *Encoder) Int64(fieldNum uint32, v int64) {
	e.tag(fieldNum, Varint)
	e.varint(uint64(v))
}

func (e *Encoder) Uint64(fieldNum uint32, v uint64) {
	e.tag(fieldNum, Varint)
	e.varint(v)
}

func (e *Encoder) Fixed64(fieldNum uint32, v uint64) {
	e.tag(fieldNum, Fixed64)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	e.buf = append(e.buf, buf[:]...)
}

func (e *Encoder) Bool(fieldNum uint32, v bool) {
	if v {
		e.tag(fieldNum, Varint)
		e.varint(1)
	}
}

func (e *Encoder) String(fieldNum uint32, v string) {
	if len(v) == 0 {
		return
	}
	e.tag(fieldNum, LengthDelimited)
	e.varint(uint64(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *Encoder) WriteBytes(fieldNum uint32, v []byte) {
	if len(v) == 0 {
		return
	}
	e.tag(fieldNum, LengthDelimited)
	e.varint(uint64(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *Encoder) Message(fieldNum uint32, v []byte) {
	if len(v) == 0 {
		return
	}
	e.tag(fieldNum, LengthDelimited)
	e.varint(uint64(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *Encoder) SubMessage(fieldNum uint32, fn func(enc *NestedEncoder)) {
	ne := &NestedEncoder{parent: e, fieldNum: fieldNum, start: len(e.buf)}
	fn(ne)
	ne.Close()
}

func (e *Encoder) RepeatedInt32(fieldNum uint32, vals []int32) {
	for _, v := range vals {
		e.Int32(fieldNum, v)
	}
}

func (e *Encoder) RepeatedMessage(fieldNum uint32, vals [][]byte) {
	for _, v := range vals {
		e.Message(fieldNum, v)
	}
}

func (e *Encoder) PackedInt32(fieldNum uint32, vals []int32) {
	if len(vals) == 0 {
		return
	}
	packed := make([]byte, 0, len(vals)*5)
	for _, v := range vals {
		packed = appendVarint(packed, uint64(v))
	}
	e.tag(fieldNum, LengthDelimited)
	e.varint(uint64(len(packed)))
	e.buf = append(e.buf, packed...)
}

func (e *Encoder) Enum(fieldNum uint32, v int32) {
	e.Int32(fieldNum, v)
}

func appendVarint(buf []byte, v uint64) []byte {
	for {
		b := byte(v) & 0x7f
		v >>= 7
		if v != 0 {
			buf = append(buf, b|0x80)
		} else {
			buf = append(buf, b)
			break
		}
	}
	return buf
}

type NestedEncoder struct {
	parent   *Encoder
	fieldNum uint32
	start    int
}

func (ne *NestedEncoder) Int32(fieldNum uint32, v int32)        { ne.parent.Int32(fieldNum, v) }
func (ne *NestedEncoder) Int64(fieldNum uint32, v int64)        { ne.parent.Int64(fieldNum, v) }
func (ne *NestedEncoder) Uint64(fieldNum uint32, v uint64)      { ne.parent.Uint64(fieldNum, v) }
func (ne *NestedEncoder) Fixed64(fieldNum uint32, v uint64)     { ne.parent.Fixed64(fieldNum, v) }
func (ne *NestedEncoder) Bool(fieldNum uint32, v bool)          { ne.parent.Bool(fieldNum, v) }
func (ne *NestedEncoder) String(fieldNum uint32, v string)      { ne.parent.String(fieldNum, v) }
func (ne *NestedEncoder) WriteBytes(fieldNum uint32, v []byte)  { ne.parent.WriteBytes(fieldNum, v) }
func (ne *NestedEncoder) Message(fieldNum uint32, v []byte)     { ne.parent.Message(fieldNum, v) }
func (ne *NestedEncoder) Enum(fieldNum uint32, v int32)         { ne.parent.Enum(fieldNum, v) }
func (ne *NestedEncoder) Uint32Field(fieldNum uint32, v uint32) { ne.parent.Uint32Field(fieldNum, v) }
func (ne *NestedEncoder) RepeatedInt32(fieldNum uint32, vals []int32) {
	ne.parent.RepeatedInt32(fieldNum, vals)
}
func (ne *NestedEncoder) RepeatedMessage(fieldNum uint32, vals [][]byte) {
	ne.parent.RepeatedMessage(fieldNum, vals)
}
func (ne *NestedEncoder) PackedInt32(fieldNum uint32, vals []int32) {
	ne.parent.PackedInt32(fieldNum, vals)
}
func (ne *NestedEncoder) SubMessage(fieldNum uint32, fn func(enc *NestedEncoder)) {
	ne.parent.SubMessage(fieldNum, fn)
}

func (ne *NestedEncoder) Close() {
	data := make([]byte, len(ne.parent.buf)-ne.start)
	copy(data, ne.parent.buf[ne.start:])
	ne.parent.buf = ne.parent.buf[:ne.start]
	ne.parent.tag(ne.fieldNum, LengthDelimited)
	ne.parent.varint(uint64(len(data)))
	ne.parent.buf = append(ne.parent.buf, data...)
}

type Decoder struct {
	buf []byte
	pos int
}

func NewDecoder(data []byte) *Decoder {
	return &Decoder{buf: data, pos: 0}
}

func (d *Decoder) Remaining() int { return len(d.buf) - d.pos }

func (d *Decoder) readVarint() (uint64, error) {
	var v uint64
	var shift uint
	for i := 0; i < 10; i++ {
		if d.pos >= len(d.buf) {
			return 0, fmt.Errorf("unexpected EOF reading varint")
		}
		b := d.buf[d.pos]
		d.pos++
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v, nil
		}
		shift += 7
	}
	return 0, fmt.Errorf("varint too long")
}

func (d *Decoder) readBytes(n int) ([]byte, error) {
	if d.pos+n > len(d.buf) {
		return nil, fmt.Errorf("unexpected EOF reading %d bytes", n)
	}
	data := d.buf[d.pos : d.pos+n]
	d.pos += n
	return data, nil
}

func (d *Decoder) ReadTag() (fieldNum uint32, wireType int, err error) {
	v, err := d.readVarint()
	if err != nil {
		return 0, 0, err
	}
	fieldNum = uint32(v >> 3)
	wireType = int(v & 0x7)
	return fieldNum, wireType, nil
}

func (d *Decoder) ReadVarint() (uint64, error) {
	return d.readVarint()
}

func (d *Decoder) ReadInt32() (int32, error) {
	v, err := d.readVarint()
	return int32(v), err
}

func (d *Decoder) ReadInt64() (int64, error) {
	v, err := d.readVarint()
	return int64(v), err
}

func (d *Decoder) ReadUint64() (uint64, error) {
	return d.readVarint()
}

func (d *Decoder) ReadFixed64() (uint64, error) {
	raw, err := d.readBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(raw), nil
}

func (d *Decoder) ReadBool() (bool, error) {
	v, err := d.readVarint()
	return v != 0, err
}

func (d *Decoder) ReadString() (string, error) {
	length, err := d.readVarint()
	if err != nil {
		return "", err
	}
	data, err := d.readBytes(int(length))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (d *Decoder) ReadBytes() ([]byte, error) {
	length, err := d.readVarint()
	if err != nil {
		return nil, err
	}
	return d.readBytes(int(length))
}

func (d *Decoder) ReadMessage() ([]byte, error) {
	length, err := d.readVarint()
	if err != nil {
		return nil, err
	}
	return d.readBytes(int(length))
}

func (d *Decoder) Skip(wireType int) error {
	switch wireType {
	case Varint:
		_, err := d.readVarint()
		return err
	case Fixed64:
		_, err := d.readBytes(8)
		return err
	case LengthDelimited:
		length, err := d.readVarint()
		if err != nil {
			return err
		}
		_, err = d.readBytes(int(length))
		return err
	case Fixed32:
		_, err := d.readBytes(4)
		return err
	default:
		return fmt.Errorf("unknown wire type %d", wireType)
	}
}

func (d *Decoder) ReadPackedInt32() ([]int32, error) {
	data, err := d.ReadMessage()
	if err != nil {
		return nil, err
	}
	sub := NewDecoder(data)
	var vals []int32
	for sub.Remaining() > 0 {
		v, err := sub.readVarint()
		if err != nil {
			return nil, err
		}
		vals = append(vals, int32(v))
	}
	return vals, nil
}

func (d *Decoder) DecodeMessage(fn func(fieldNum uint32, wireType int) error) error {
	for d.Remaining() > 0 {
		fNum, wt, err := d.ReadTag()
		if err != nil {
			return err
		}
		if err := fn(fNum, wt); err != nil {
			return err
		}
	}
	return nil
}

// UUID helpers

func EncodeUUID(fieldNum uint32, uuidStr string, e *Encoder) {
	var msb, lsb uint64
	if len(uuidStr) == 36 {
		hex := ""
		for _, c := range uuidStr {
			if c != '-' {
				hex += string(c)
			}
		}
		if len(hex) == 32 {
			msb, _ = hexToUint64(hex[:16])
			lsb, _ = hexToUint64(hex[16:])
		}
	}
	e.SubMessage(fieldNum, func(enc *NestedEncoder) {
		enc.Fixed64(1, msb)
		enc.Fixed64(2, lsb)
	})
}

func hexToUint64(hex string) (uint64, error) {
	var v uint64
	for _, c := range hex {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v |= uint64(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex char: %c", c)
		}
	}
	return v, nil
}

// Color helpers - colors are uint32 ARGB, protobuf int32 uses signed varint
func EncodeColor(fieldNum uint32, argb uint32, e *Encoder) {
	e.SubMessage(fieldNum, func(enc *NestedEncoder) {
		enc.Uint32Field(1, argb)
	})
}

func EncodeEmptyColor() []byte {
	e := NewEncoder()
	e.Uint32Field(1, 0)
	return e.Bytes()
}

// Timestamp
func EncodeTimestamp(fieldNum uint32, unixSeconds int64, nanos int32, e *Encoder) {
	e.SubMessage(fieldNum, func(enc *NestedEncoder) {
		enc.Int64(1, unixSeconds)
		enc.Int32(2, nanos)
	})
}

// OwnedCosmetic
func EncodeOwnedCosmetic(cosmeticID int32) []byte {
	e := NewEncoder()
	e.Int32(1, cosmeticID)
	EncodeTimestamp(2, 1717000000, 0, e)
	return e.Bytes()
}

// EquippedCosmetic
func EncodeEquippedCosmetic(cosmeticID int32) []byte {
	e := NewEncoder()
	e.Int32(1, cosmeticID)
	return e.Bytes()
}

// EncodeEmptyStruct
func EncodeEmptyStruct() []byte {
	return NewEncoder().Bytes()
}

var _ = binary.BigEndian
var _ = math.MaxInt32
