package dawg

import (
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

var maskTop = []byte{
	0xff,
	0x7f,
	0x3f,
	0x1f,
	0x0f,
	0x07,
	0x03,
	0x01,
	0x00,
}

// BitSeeker reads bits from a given offset in bits
type bitSeeker struct {
	io.ReaderAt
	p      int64
	buffer []byte
}

// NewBitSeeker creates a new bitreaderat
func newBitSeeker(r io.ReaderAt) *bitSeeker {
	return &bitSeeker{r, 0, make([]byte, 1, 1)}
}

func (r *bitSeeker) nextByte() byte {
	r.ReadAt(r.buffer, r.p>>3)
	return r.buffer[0]
}

func (r *bitSeeker) ReadBits(n int64) uint64 {
	if r.p&7+n <= 8 {
		ret := uint64((r.nextByte() & maskTop[r.p&7]) >> (8 - r.p&7 - n))
		r.p += n
		return ret
	}

	// case 2: bits lie incompletely in the given byte
	var result uint64
	result = uint64((r.nextByte() & maskTop[r.p&7]))

	l := 8 - r.p&7
	r.p += l
	n -= l

	for n >= 8 {
		result = (result << 8) | uint64(r.nextByte())
		r.p += 8
		n -= 8
	}

	if n > 0 {
		r.p += n
		result = (result << n) | uint64(r.nextByte()>>(8-n))
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
