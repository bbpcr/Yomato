package bitfield

import (
	"bytes"
)

// A bitfield is defined as here:
// https://wiki.theory.org/BitTorrentSpecification#bitfield:_.3Clen.3D0001.2BX.3E.3Cid.3D5.3E.3Cbitfield.3E
type Bitfield struct {
	bytes  []uint8
	length uint
}

func New(length int) Bitfield {

	if length % 8 == 0 {
		bitfield := Bitfield{make([]uint8, length/8), uint(length)}
		return bitfield
	} else {
		bitfield := Bitfield{make([]uint8, length/8+1), uint(length)}
		return bitfield
	}
}

// Return true if the position `pos` is ON and false otherwise
func (bitfield Bitfield) At(pos int) bool {

	if (uint(pos / 8) >= bitfield.length) {
		return false
	}

	num := bitfield.bytes[pos/8]
	val := num & (1 << uint(pos%8))
	return (val != 0)
}

// Sets a position ON or OFF
func (bitfield Bitfield) Set(pos int, val bool) {

	if (uint(pos / 8) >= bitfield.length) {
		return
	}

	num := bitfield.bytes[pos/8]
	mask := uint8(1 << (7 - uint(pos%8)))
	if val {
		num = num | mask
	} else {
		num = num & ^mask
	}
	bitfield.bytes[pos/8] = num
}

// Puts the bytes into bitfield. This isn't a regular copy , instead it or's with all bytes.
func (bitfield Bitfield) Put(bytes []uint8 , count int) {
	
	if count < 0 {
		return
	}
	
	for index := 0 ; index < count ; index ++ {
		bitfield.bytes[index] |= bytes[index]
	}
}

// Dumps the bitfield into a human-readable form with 0 and 1
func (bitfield Bitfield) Dump() string {
	var buf bytes.Buffer
	for idx, num := range bitfield.bytes {
		for i := 7; i >= 0; i-- {
			// don't zero-pad in the human readable form, only display
			// the exact number of bits stored
			if idx*8+(8-i) > int(bitfield.length) {
				break
			}
			if num&(1<<uint(i)) != 0 {
				buf.WriteString("1")
			} else {
				buf.WriteString("0")
			}
		}
	}
	return buf.String()
}

// Dumps the bitfield into a list of bytes, where index 0
// is the most significant bit of the first byte, and so on
// zero-pad at the end, as needed
func (bitfield Bitfield) Encode() []byte {
	var buf bytes.Buffer
	for _, num := range bitfield.bytes {
		buf.WriteByte(byte(num))
	}
	return buf.Bytes()
}
