package font

import (
	"bytes"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// Reserving the output must still account for arbitrary table padding and must
// not change the serialized tables, checksum, source font or repeated writes.
func TestSFNTWriteReservesCompleteAlignedOutput(t *testing.T) {
	face, err := ParseSFNT(bytes.Clone(goregular.TTF), 0)
	if err != nil {
		t.Fatal(err)
	}
	face.Tables["TEST"] = []byte{1, 2, 3}
	face.Tables["TST2"] = []byte{4, 5, 6, 7, 8}
	head := bytes.Clone(face.Tables["head"])
	output := face.Write()
	if cap(output) != len(output) || len(output)%4 != 0 {
		t.Fatalf("output was grown or is not aligned: len=%d cap=%d", len(output), cap(output))
	}
	if calcChecksum(output) != 0xb1b0afba {
		t.Fatal("font checksum changed")
	}
	roundtrip, err := ParseSFNT(output, 0)
	if err != nil {
		t.Fatal(err)
	}
	for tag, expected := range face.Tables {
		if tag != "head" && !bytes.Equal(roundtrip.Tables[tag], expected) {
			t.Fatalf("table %s changed", tag)
		}
	}
	if !bytes.Equal(face.Tables["head"], head) || !bytes.Equal(face.Write(), output) {
		t.Fatal("writing mutated the source or changed repeated output")
	}
}

func BenchmarkSFNTWrite(b *testing.B) {
	face, err := ParseSFNT(bytes.Clone(goregular.TTF), 0)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = face.Write()
	}
}
