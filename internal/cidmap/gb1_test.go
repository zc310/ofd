package cidmap

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestGB1CompleteMappingFingerprints(t *testing.T) {
	glyphHash, textHash := sha256.New(), sha256.New()
	var word [4]byte
	for cid := range 65536 {
		r, ok := GB1GlyphUnicode(cid)
		if !ok {
			r = 0
		}
		binary.BigEndian.PutUint32(word[:], uint32(r))
		glyphHash.Write(word[:])
		text, ok := GB1Text(cid)
		if !ok {
			text = "\ufffd"
		}
		binary.BigEndian.PutUint32(word[:], uint32(len(text)))
		textHash.Write(word[:])
		textHash.Write([]byte(text))
	}
	if got := fmt.Sprintf("%x", glyphHash.Sum(nil)); got != "8ba3ea0e86d4cbd03eb9ecab98eb2b470d4f8a10df2306cf90359328135598df" {
		t.Fatalf("glyph mapping fingerprint = %s", got)
	}
	if got := fmt.Sprintf("%x", textHash.Sum(nil)); got != "371b4cf0c50dc00db8f6d0e941ee4ef73755d8f8f23133773680de88ba6ada03" {
		t.Fatalf("text mapping fingerprint = %s", got)
	}
	for _, cid := range []int{-1, 30284, 65536} {
		if _, ok := GB1Text(cid); ok {
			t.Fatalf("out-of-range CID %d accepted", cid)
		}
	}
}
