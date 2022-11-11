package dawg

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/bits"
	"os"

	"golang.org/x/exp/mmap"
)

/* FILE FORMAT
- 4 bytes - total size of file
- 1 byte: cbits
- 1 byte: abits
- 7code - number of words
- 7code - number of nodes
- 7code - number of edges
- let wbits be the number of bits to represent the total number of words in the file.
- for each node:
	- 1 bit: is node final?
	- 1 bit: fallthrough?

	- if fallthrough
		cbits: character
	else:
		1 bit: single edge?
		- if !single edge:
			7code: number of edges
			log(wbits): nskip (number of bits in skip field)
		- for each edge:
			cbits: character
			if this is not the first edge:
				nskip: count
			abits: location in bits of the node to jump to from start of file.

We define 7code to be an unsigned that can be read the following way:

result = 0
for {
	data = next 8 bits
	result = result << 7 | data & 0x7f
	if data & 0x80 == 0 break
}

*/

// Save writes the dawg to disk. Returns the number of bytes written
func (d *dawg) Save(filename string) (int64, error) {
	d.checkFinished()

	f, err := os.Create(filename)
	if err != nil {
		return 0, err
	}

	defer f.Close()
	return d.Write(f)
}

func readUint32(r io.ReaderAt, at int64) uint32 {
	data := make([]byte, 4)
	_, err := r.ReadAt(data, at)
	if err != nil {
		log.Panic(err)
	}
	return (uint32(data[0]) << 24) |
		(uint32(data[1]) << 16) |
		(uint32(data[2]) << 8) |
		(uint32(data[3]) << 0)
}

func (n *node) isFallthrough(id int) bool {
	return len(n.edges) == 1 && n.edges[0].node == id+1
}

// Save writes the dawg to an io.Writer. Returns the number of bytes written
func (d *dawg) Write(wIn io.Writer) (int64, error) {
	if d.r != nil {
		return io.Copy(wIn, io.NewSectionReader(d.r, 0, d.size))
	}

	if !d.finished {
		return 0, errors.New("dawg not finished")
	}

	w := newBitWriter(wIn)

	// get maximum character and calculate cbits
	// record node addresses, calculate counts and number of edges
	addresses := make([]uint64, d.NumNodes())
	var maxChar rune
	for _, node := range d.nodes {
		for _, edge := range node.edges {
			if edge.ch > maxChar {
				maxChar = edge.ch
			}
		}
	}

	cbits := uint64(bits.Len(uint(maxChar)))
	wbits := uint64(bits.Len(uint(d.NumAdded())))
	nskiplen := uint64(bits.Len(uint(wbits)))

	// let abits = 1
	abits := uint64(1)
	var pos uint64
	for {
		// position = 32 + 8 + 8 + encoded length of number of words, nodes, and edges
		pos = 32 + 8 + 8
		pos += unsignedLength(uint64(d.NumAdded())) * 8
		pos += unsignedLength(uint64(d.NumNodes())) * 8
		pos += unsignedLength(uint64(d.NumEdges())) * 8

		// for each node,
		for i := range addresses {
			node := d.nodes[i]

			// record its position
			addresses[i] = pos

			// final bit
			pos++

			// fallthrough?
			pos++

			if node.isFallthrough((i)) {
				pos += cbits
			} else {
				// add number of edges
				pos++ // singleEdge?

				numEdges := uint64(len(node.edges))

				// find maximum value of skip

				skip := 0
				if node.final {
					skip = 1
				}

				for _, edge := range node.edges {
					skip += d.nodes[edge.node].count
				}

				nskipbits := uint64(bits.Len(uint(skip)))

				if numEdges != 1 {
					pos += unsignedLength(numEdges) * 8
					pos += nskiplen
				}

				// add #edges * (cbits + wbits + abits)
				if numEdges > 0 {
					pos += numEdges*(cbits+nskipbits+abits) - nskipbits
				}
			}
		}

		// if file position fits into abits, then break out.
		if uint64(bits.Len(uint(pos))) <= abits {
			break
		}
		abits = uint64(bits.Len(uint(pos)))
	}

	size := (pos + 7) / 8

	// write file size, cbits, abits
	w.WriteBits(size, 32)
	w.WriteBits(cbits, 8)
	w.WriteBits(abits, 8)

	// write number of words, nodes, and edges.
	writeUnsigned(w, uint64(d.NumAdded()))
	writeUnsigned(w, uint64(d.NumNodes()))
	writeUnsigned(w, uint64(d.NumEdges()))

	// for each edge,
	for i := range addresses {
		node := d.nodes[i]
		count := 0
		if node.final {
			count++
			w.WriteBits(1, 1)
		} else {
			w.WriteBits(0, 1)
		}

		if node.isFallthrough(i) {
			w.WriteBits(1, 1)
			w.WriteBits(uint64(node.edges[0].ch), int(cbits))
		} else {
			w.WriteBits(0, 1)
			skip := 0
			if node.final {
				skip = 1
			}

			for _, edge := range node.edges {
				skip += d.nodes[edge.node].count
			}

			nskipbits := uint64(bits.Len(uint(skip)))

			if len(node.edges) == 1 {
				w.WriteBits(1, 1)
			} else {
				w.WriteBits(0, 1)
				writeUnsigned(w, uint64(len(node.edges)))
				w.WriteBits(nskipbits, int(nskiplen))
			}

			for index, edge := range node.edges {
				// write character, address
				w.WriteBits(uint64(edge.ch), int(cbits))
				if index > 0 {
					w.WriteBits(uint64(count), int(nskipbits))
				}
				w.WriteBits(addresses[edge.node], int(abits))
				count += d.nodes[edge.node].count
			}
		}
	}

	w.Flush()

	return int64(size), nil
}

// Load loads the dawg from a file
func Load(filename string) (Finder, error) {
	f, err := mmap.Open(filename)
	if err != nil {
		return nil, err
	}

	return Read(f, 0)
}

// Read returns a finder that accesses the dawg in-place using the
// given io.ReaderAt
func Read(f io.ReaderAt, offset int64) (Finder, error) {
	size := readUint32(f, offset)
	if offset != 0 {
		f = io.NewSectionReader(f, offset, int64(size))
	}

	r := newBitSeeker(f)

	r.Seek(32, 0)
	cbits := r.ReadBits(8)
	abits := r.ReadBits(8)
	numAdded := int(readUnsigned(&r))
	numNodes := int(readUnsigned(&r))
	numEdges := int(readUnsigned(&r))
	firstNodeOffset := r.Tell()
	hasEmpty := r.ReadBits(1) == 1
	wbits := int64(bits.Len(uint(numAdded)))
	dawg := &dawg{
		finished:        true,
		numAdded:        numAdded,
		numNodes:        numNodes,
		numEdges:        numEdges,
		abits:           int64(abits),
		cbits:           int64(cbits),
		wbits:           wbits,
		hasEmptyWord:    hasEmpty,
		firstNodeOffset: firstNodeOffset,
		r:               f,
		size:            int64(size),
	}

	return dawg, nil
}

// Close ...
func (d *dawg) Close() error {
	if closer, ok := d.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (d *dawg) getEdge(r *bitSeeker, eStart edgeStart) (edgeEnd, bool, bool) {
	var edgeEnd edgeEnd
	var final, ok bool
	if d.numEdges > 0 {
		pos := int64(eStart.node)
		if pos == 0 {
			// its the first node
			pos = d.firstNodeOffset
		}

		r.Seek(pos, 0)
		nodeFinal := int(r.ReadBits(1))
		fallthr := int(r.ReadBits(1))

		if fallthr == 1 {
			ch := rune(r.ReadBits(d.cbits))
			if ch == eStart.ch {
				edgeEnd.count = nodeFinal
				edgeEnd.node = int(r.Tell())
				final = r.ReadBits(1) == 1
				ok = true
			}
		} else {
			singleEdge := r.ReadBits(1)
			numEdges := uint64(1)
			nskiplen := int64(bits.Len(uint(d.wbits)))
			nskip := int64(0)
			if singleEdge != 1 {
				numEdges = readUnsigned(r)
				nskip = int64(r.ReadBits(nskiplen))
			}

			pos = r.Tell()
			bsearch(int(numEdges), func(i int) int {
				seekTo := pos + int64(i)*int64(d.cbits+nskip+d.abits)
				if i > 0 {
					seekTo -= nskip
				}

				r.Seek(seekTo, 0)
				ch := rune(r.ReadBits(d.cbits))
				if ch == eStart.ch {
					if i > 0 {
						edgeEnd.count = int(r.ReadBits(nskip))
					} else {
						edgeEnd.count = nodeFinal
					}
					edgeEnd.node = int(r.ReadBits(d.abits))
					r.Seek(int64(edgeEnd.node), 0)
					final = r.ReadBits(1) == 1
					ok = true
				}
				return int(ch - eStart.ch)
			})
		}
	}

	return edgeEnd, final, ok
}

type nodeResult struct {
	node  int
	final bool
	edges []edgeResult
}

type edgeResult struct {
	ch    rune
	count int
	node  int
}

func (d *dawg) getNode(r *bitSeeker, node int) nodeResult {
	var result nodeResult
	pos := int64(node)
	if pos == 0 {
		// its the first node
		pos = d.firstNodeOffset
	}

	r.Seek(pos, 0)
	nodeFinal := r.ReadBits(1)
	fallthr := r.ReadBits(1)

	result.node = node
	result.final = nodeFinal == 1

	if fallthr == 1 {
		result.edges = append(result.edges, edgeResult{
			ch:    rune(r.ReadBits(d.cbits)),
			count: int(nodeFinal),
			node:  int(r.Tell()),
		})
	} else {
		nskiplen := int64(bits.Len(uint(d.wbits)))
		nskip := int64(0)

		singleEdge := r.ReadBits(1)
		numEdges := uint64(1)
		if singleEdge != 1 {
			numEdges = readUnsigned(r)
			nskip = int64(r.ReadBits(nskiplen))
		}

		for i := uint64(0); i < numEdges; i++ {
			ch := r.ReadBits(int64(d.cbits))
			var count uint64
			if i > 0 {
				count = r.ReadBits(int64(nskip))
			} else {
				count = nodeFinal
			}
			address := r.ReadBits(int64(d.abits))
			result.edges = append(result.edges, edgeResult{
				ch:    rune(ch),
				count: int(count),
				node:  int(address),
			})
		}
	}
	return result
}

// DumpFile prints out the file
func DumpFile(f io.ReaderAt) {
	r := newBitSeeker(f)
	size := r.ReadBits(32)
	fmt.Printf("[%08x] Size=%v bytes\n", r.Tell()-32, size)

	cbits := r.ReadBits(8)
	fmt.Printf("[%08x] cbits=%d\n", r.Tell()-8, cbits)

	abits := r.ReadBits(8)
	fmt.Printf("[%08x] abits=%d\n", r.Tell()-8, cbits)

	wordCount := readUnsigned(&r)
	fmt.Printf("[%08x] WordCount=%v\n", r.Tell()-int64(unsignedLength(wordCount)*8), wordCount)

	nodeCount := readUnsigned(&r)
	fmt.Printf("[%08x] NodeCount=%v\n", r.Tell()-int64(unsignedLength(nodeCount)*8), nodeCount)
	wbits := bits.Len(uint(wordCount))

	edgeCount := readUnsigned(&r)
	fmt.Printf("[%08x] EdgeCount=%v\n", r.Tell()-int64(unsignedLength(edgeCount)*8), edgeCount)

	nskiplen := bits.Len(uint(wbits))

	for i := 0; i < int(nodeCount); i++ {
		at := r.Tell()
		final := r.ReadBits(1)
		fallthr := r.ReadBits(1)

		if fallthr == 1 {
			ch := r.ReadBits(int64(cbits))
			fmt.Printf("[%08x] Node final=%d ch='%c' (fallthrough)\n", at, final, rune(ch))
			continue
		}

		singleEdge := r.ReadBits(1)
		edges := uint64(1)
		nskip := uint64(0)
		if singleEdge != 1 {
			edges = readUnsigned(&r)
			nskip = r.ReadBits(int64(nskiplen))
		}

		fmt.Printf("[%08x] Node final=%d has %d edges, skipfieldlen=%d\n", at, final, edges, nskip)

		for j := uint64(0); j < edges; j++ {
			at = r.Tell()
			ch := r.ReadBits(int64(cbits))
			var count uint64
			if j > 0 {
				count = r.ReadBits(int64(nskip))
			} else {
				count = final
			}
			address := r.ReadBits(int64(abits))
			fmt.Printf("[%08x] '%c' goto <%08x> skipping %d\n",
				at, rune(ch), address, count)
		}

	}
}

func writeUnsigned(w *bitWriter, n uint64) {
	if n < 0x7f {
		w.WriteBits(n, 8)
	} else if n < 0x3fff {
		w.WriteBits((n>>7)&0x7f|0x80, 8)
		w.WriteBits(n&0x7f, 8)
	} else if n < 0x1fffff {
		w.WriteBits((n>>14)&0x7f|0x80, 8)
		w.WriteBits((n>>7)&0x7f|0x80, 8)
		w.WriteBits(n&0x7f, 8)
	} else if n < 0xfffffff {
		w.WriteBits((n>>21)&0x7f|0x80, 8)
		w.WriteBits((n>>14)&0x7f|0x80, 8)
		w.WriteBits((n>>7)&0x7f|0x80, 8)
		w.WriteBits(n&0x7f, 8)
	} else {
		// could go further
		log.Panic("Not implemented")
	}
}

func readUnsigned(r *bitSeeker) uint64 {
	var result uint64
	for {
		d := r.ReadBits(8)
		result = (result << 7) | d&0x7f
		if d&0x80 == 0 {
			break
		}
	}
	return result
}

func unsignedLength(n uint64) uint64 {
	if n < 0x7f {
		return 1
	} else if n < 0x3fff {
		return 2
	} else if n < 0x1fffff {
		return 3
	} else if n < 0xfffffff {
		return 4
	}
	log.Panicf("Not implemented: %d", n)
	return 0
}

/** @param cmp returns target - i  or cmp(i, target)*/
func bsearch(count int, cmp func(i int) int) int {
	high := count
	low := -1
	var match, probe int
	for high-low > 1 {
		probe = (high + low) >> 1

		match = cmp(probe)

		if match == 0 {
			return probe
		} else if match < 0 {
			low = probe
		} else {
			high = probe
		}
	}

	return high
}
