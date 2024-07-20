package mem

import (
	"bytes"
	"testing"
)

func TestSplit(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := NewBuffer(data, func(bytes []byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	})
	checkBufData := func(b *Buffer, expected []byte) {
		if !bytes.Equal(b.ReadOnlyData(), expected) {
			t.Fatalf("Buffer did not contain expected data %v, got %v", expected, b.ReadOnlyData())
		}
	}

	ref1 := buf.Ref()

	split := buf.Split(2)
	checkBufData(buf, data[:2])
	checkBufData(split, data[2:])
	checkBufData(ref1, data)

	splitRef := split.Ref()
	ref2 := buf.Ref()
	split.Free()
	buf.Free()
	ref1.Free()
	splitRef.Free()

	ready = true
	ref2.Free()

	if !freed {
		t.Fatalf("Buffer never freed")
	}
}
