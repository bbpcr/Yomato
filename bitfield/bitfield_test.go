package bitfield

import (
	"fmt"
	"testing"
)

func TestBasicBitfield(t *testing.T) {
	b := New(10)
	b.Set(0, true)
	b.Set(9, true)
	b.Set(0, false)
	if b.Dump() != "0000000001" {
		t.Errorf("Bitfield not setting bits correctly: %s", b.Dump())
	}
	if b.At(9) != true {
		t.Errorf("Bitfield At method not working correctly.")
	}

	fmt.Println(b.OneBits, " ", b.ZeroBits)

	if b.OneBits != 1 {
		t.Errorf("Bitfield not setting the number of one bits correctly: Have %d , expected 1", b.OneBits)
	}
}
