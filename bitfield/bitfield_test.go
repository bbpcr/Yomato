package bitfield

import (
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
}
