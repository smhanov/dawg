package dawg

import (
	"bytes"
	"testing"
)

func TestBitWriter(t *testing.T) {
	// write 101010 = 0x2a
	// write 010101 = 0x15
	// result: 10101001 01010000 = 0xa9 0x50
	var buffer bytes.Buffer
	bw := newBitWriter(&buffer)
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
	br := newBitSeeker(buffer)

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

	t.Logf("Done TestBitReader")
}

func TestBitReaderWriter(t *testing.T) {
	var buffer bytes.Buffer
	bw := newBitWriter(&buffer)

	for i := 0; i < 100000; i++ {
		bits := i % 31
		data := i & ((1 << bits) - 1)
		bw.WriteBits(uint64(data), bits)
	}

	bw.Flush()

	r := bytes.NewReader(buffer.Bytes())
	br := newBitSeeker(r)

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
