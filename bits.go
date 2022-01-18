package dawg

import (
	"encoding/binary"
	"io"
	"log"
)

type bitWriter struct {
	io.Writer
	cache uint8
	used  int
}

// NewBitWriter creates a new BitWriter from an io writer.
func newBitWriter(w io.Writer) *bitWriter {
	return &bitWriter{w, 0, 0}
}

func (w *bitWriter) WriteBits(data uint64, n int) error {
	var mask uint8
	for n > 0 {
		written := n
		if written+w.used > 8 {
			written = 8 - w.used
		}

		mask = uint8(uint16(1<<(written)) - 1)
		w.used += written
		w.cache = (w.cache << written) | byte(data>>(n-written))&mask

		if w.used == 8 {
			_, err := w.Write([]byte{w.cache})
			if err != nil {
				return err
			}
			w.used = 0
		}

		n -= written
	}
	return nil
}

func (w *bitWriter) Flush() error {
	if w.used > 0 {
		_, err := w.Write([]byte{w.cache << (8 - w.used)})
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *bitWriter) Close() error {
	if err := w.Flush(); err != nil {
		return err
	}

	if closer, ok := w.Writer.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

var maskTop = [64]uint64{
	0xffffffffffffffff,
	0x7fffffffffffffff,
	0x3fffffffffffffff,
	0x1fffffffffffffff,
	0x0fffffffffffffff,
	0x07ffffffffffffff,
	0x03ffffffffffffff,
	0x01ffffffffffffff,
	0x00ffffffffffffff,
	0x007fffffffffffff,
	0x003fffffffffffff,
	0x001fffffffffffff,
	0x000fffffffffffff,
	0x0007ffffffffffff,
	0x0003ffffffffffff,
	0x0001ffffffffffff,
	0x0000ffffffffffff,
	0x00007fffffffffff,
	0x00003fffffffffff,
	0x00001fffffffffff,
	0x00000fffffffffff,
	0x000007ffffffffff,
	0x000003ffffffffff,
	0x000001ffffffffff,
	0x000000ffffffffff,
	0x0000007fffffffff,
	0x0000003fffffffff,
	0x0000001fffffffff,
	0x0000000fffffffff,
	0x00000007ffffffff,
	0x00000003ffffffff,
	0x00000001ffffffff,
	0x00000000ffffffff,
	0x000000007fffffff,
	0x000000003fffffff,
	0x000000001fffffff,
	0x000000000fffffff,
	0x0000000007ffffff,
	0x0000000003ffffff,
	0x0000000001ffffff,
	0x0000000000ffffff,
	0x00000000007fffff,
	0x00000000003fffff,
	0x00000000001fffff,
	0x00000000000fffff,
	0x000000000007ffff,
	0x000000000003ffff,
	0x000000000001ffff,
	0x000000000000ffff,
	0x0000000000007fff,
	0x0000000000003fff,
	0x0000000000001fff,
	0x0000000000000fff,
	0x00000000000007ff,
	0x00000000000003ff,
	0x00000000000001ff,
	0x00000000000000ff,
	0x000000000000007f,
	0x000000000000003f,
	0x000000000000001f,
	0x000000000000000f,
	0x0000000000000007,
	0x0000000000000003,
	0x0000000000000001,
}

// BitSeeker reads bits from a given offset in bits
type bitSeeker struct {
	io.ReaderAt
	p      int64
	have   int64
	buffer [8]byte
	slice  []byte
	cache  uint64
}

// NewBitSeeker creates a new bitreaderat
func newBitSeeker(r io.ReaderAt) bitSeeker {
	bs := bitSeeker{ReaderAt: r, have: -1}
	// avoids re-creating the slice over and over.
	bs.slice = bs.buffer[:]
	return bs
}

func (r *bitSeeker) nextWord(at int64) uint64 {
	at = at >> 6
	if at != r.have {
		r.ReadAt(r.slice, at<<3)
		r.have = at
		r.cache = binary.BigEndian.Uint64(r.slice)
	}
	return r.cache
}

func (r *bitSeeker) ReadBits(n int64) uint64 {
	var result uint64

	p := r.p & 63
	//mask := uint64((1 << (64 - uint8(p))) - 1)
	mask := maskTop[p]
	if p+n <= 64 {
		result = (r.nextWord(r.p) & mask) >> (64 - p - n)
		r.p += n
		return result
	}

	// case 2: bits lie incompletely in the given byte
	result = r.nextWord(r.p) & mask

	l := 64 - p
	r.p += l
	n -= l

	if n > 0 {
		r.p += n
		result = (result << n) | r.nextWord(r.p)>>(64-n)
	}

	return result

}

func (r *bitSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.p = offset
	case io.SeekCurrent:
		r.p += offset
	default:
		log.Panicf("Seek whence=%d not supported", whence)
	}
	return r.p, nil
}

func (r *bitSeeker) Skip(offset int64) {
	r.p += offset
}

func (r *bitSeeker) Tell() int64 {
	return r.p
}
