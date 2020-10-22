package dawg

import (
	"bytes"
	"math/rand"
	"testing"
)

func testMph(t *testing.T, words []string) {
	G, permute := CreateMinimalPerfectHash(len(words), func(d uint32, i int) uint32 {
		return StringHash(d, words[i])
	})

	words2 := make([]string, len(words), len(words))
	for dest, src := range permute {
		words2[dest] = words[src]
	}

	//for i, word := range words2 {
	//log.Printf("%d %s %d %d", G[i], word, int(hash(0, word))%len(G), int(hash(G[i], word))%len(G))
	//}

	for _, word := range words2 {
		d := G[int(StringHash(0, word))%len(G)]
		//log.Printf("Word %s hashes to %d, D=%d", word, int(hash(0, word))%len(G), d)
		var result string
		if d < 0 {
			result = words2[-d-1]
			//log.Printf("   Second hash is %d", -d-1)
		} else {
			result = words2[int(StringHash(uint32(d), word))%len(words2)]
			//log.Printf("   Second hash is %d", int(hash(d, word))%len(G))
		}

		if word != result {
			t.Errorf("Expected %s got %s", word, result)
		}
	}
}

func TestMph_5Words(t *testing.T) {
	testMph(t, []string{
		"apple",
		"banana",
		"hello",
		"how",
		"what",
	})
}

func TestMph_1Words(t *testing.T) {
	testMph(t, []string{
		"alpha",
	})
}

func TestMph_0Words(t *testing.T) {
	testMph(t, []string{})
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func TestMph_100000Words(t *testing.T) {
	var words []string
	for i := 0; i < 100000; i++ {
		words = append(words, randomString(10))
	}
	testMph(t, words)
}

func TestBitWriter(t *testing.T) {
	// write 101010 = 0x2a
	// write 010101 = 0x15
	// result: 10101001 01010000 = 0xa9 0x50
	var buffer bytes.Buffer
	bw := NewBitWriter(&buffer)
	bw.WriteBits(0x2a, 6)
	bw.WriteBits(0x15, 6)
	bw.Close()

	b := buffer.Bytes()
	if len(b) != 2 || b[0] != 0xa9 || b[1] != 0x50 {
		t.Errorf("Error: TestBitWriter wrote %v", b)
	}

	t.Logf("Passed TestBitWriter")
}

func TestBitReader(t *testing.T) {
	// result: 10101001 01010000 = 0xa9 0x50
	// read 101010 = 0x2a
	// read 010101 = 0x15
	// result: 10101001 01010000 = 0xa9 0x50
	buffer := bytes.NewReader([]byte{0xa9, 0x50})
	br := NewBitSeeker(buffer)

	data := br.ReadBits(6)
	if data != 0x2a {
		t.Errorf("Expected 0x2a got 0x%02x", data)
	}

	data = br.ReadBits(6)
	if data != 0x15 {
		t.Errorf("Expected 0x15 got 0x%02x", data)
	}

	data = br.ReadBits(2)
	if data != 0x00 {
		t.Errorf("Expected 0x00 got 0x%02x", data)
	}

	br.Seek(0, 0)
	data = br.ReadBits(16)
	if data != 0xa950 {
		t.Errorf("Expected 0xa950 got 0x%02x", data)
	}

	br.Seek(1, 0)
	// result: 0010 1001 0101 0000 = 0x2950
	data = br.ReadBits(15)
	if data != 0x2950 {
		t.Errorf("Expected 0x2950 got 0x%02x", data)
	}

	t.Logf("Passed TestBitWriter")
}

func TestBitReaderWriter(t *testing.T) {
	var buffer bytes.Buffer
	bw := NewBitWriter(&buffer)

	for i := 0; i < 100000; i++ {
		bits := i % 31
		data := i & ((1 << bits) - 1)
		bw.WriteBits(uint64(data), bits)
	}

	bw.Flush()

	r := bytes.NewReader(buffer.Bytes())
	br := NewBitSeeker(r)

	for i := 0; i < 100000; i++ {
		bits := i % 31
		data := i & ((1 << bits) - 1)

		dataRead := br.ReadBits(int64(bits))
		if int(dataRead) != data {
			t.Errorf("Fail: %d Expected 0x%x, read 0x%x", bits, data, dataRead)
		}
	}

	t.Logf("Passed TestBitReaderWriter")
}
