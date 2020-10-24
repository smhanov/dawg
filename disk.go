package dawg

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/bits"
	"os"
	"sort"

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
	- 1 bit: single edge?
	- if !single edge:
		7code: number of edges
	- for each edge:
		cbits: character
		if this is not the first edge:
			wbits: count
		abits: location in bits of the node to jump to from start of file.


We define 7code to be an unsigned that can be read the following way:
result = 0
loop {
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
	data := make([]byte, 4, 4)
	_, err := r.ReadAt(data, at)
	if err != nil {
		log.Panic(err)
	}
	return (uint32(data[0]) << 24) |
		(uint32(data[1]) << 16) |
		(uint32(data[2]) << 8) |
		(uint32(data[3]) << 0)
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

	// get all the edges
	edges := d.getEdges()

	// get maximum character and calculate cbits
	// record node addresses, calculate counts and number of edges
	type node struct {
		edgeCount uint64
		address   uint64
	}

	nodes := make([]node, d.NumNodes(), d.NumNodes())
	var maxChar rune
	for _, start := range edges {
		if start.ch > maxChar {
			maxChar = start.ch
		}
		nodes[start.node].edgeCount++
	}

	cbits := uint64(bits.Len(uint(maxChar)))
	wbits := uint64(bits.Len(uint(d.NumAdded())))

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
		for i := range nodes {
			// record its position
			nodes[i].address = pos

			// final bit
			pos++

			// add number of edges
			pos++ // edgecount == 1 bit
			if nodes[i].edgeCount != 1 {
				pos += unsignedLength(nodes[i].edgeCount) * 8
			}

			// add #edges * (cbits + wbits + abits)
			if nodes[i].edgeCount > 0 {
				pos += nodes[i].edgeCount*(cbits+wbits+abits) - wbits
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
	i := -1
	var firstEdge bool
	for _, start := range edges {
		// if its a different node, then write number of edges
		end, _, _ := d.getEdge(start)
		for i < start.node {
			i++
			if d.final[i] {
				w.WriteBits(1, 1)
			} else {
				w.WriteBits(0, 1)
			}
			if nodes[i].edgeCount == 1 {
				w.WriteBits(1, 1)
			} else {
				w.WriteBits(0, 1)
				writeUnsigned(w, nodes[i].edgeCount)
			}
			firstEdge = true
		}

		// write character, address
		w.WriteBits(uint64(start.ch), int(cbits))
		if !firstEdge {
			w.WriteBits(uint64(end.count), int(wbits))
		}
		w.WriteBits(nodes[end.node].address, int(abits))
		firstEdge = false
	}

	// if there were no edges, then write out the first node
	i++
	if i < len(nodes) {
		if d.final[i] {
			w.WriteBits(1, 1)
		} else {
			w.WriteBits(0, 1)
		}
		w.WriteBits(0, 1)
		writeUnsigned(w, 0)
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

const edgesOffset = (32*4 + 8 + 8)

// Read returns a finder that accesses the dawg in-place using the
// given io.ReaderAt
func Read(f io.ReaderAt, offset int64) (Finder, error) {
	size := readUint32(f, offset)

	r := newBitSeeker(io.NewSectionReader(f, offset, int64(size)))

	r.Seek(32, 0)
	cbits := r.ReadBits(8)
	abits := r.ReadBits(8)
	numAdded := int(readUnsigned(r))
	numNodes := int(readUnsigned(r))
	numEdges := int(readUnsigned(r))
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

func (d *dawg) getEdge(eStart edgeStart) (edgeEnd, bool, bool) {
	var edgeEnd edgeEnd
	var final, ok bool
	if d.numEdges == 0 {
		// do nothing
	} else if d.r == nil {
		edgeEnd, ok = d.edges[eStart]
		final = d.final[edgeEnd.node]
	} else {
		r := newBitSeeker(d.r)
		pos := int64(eStart.node)
		if pos == 0 {
			// its the first node
			pos = d.firstNodeOffset
		}

		r.Seek(pos, 0)
		nodeFinal := int(r.ReadBits(1))
		singleEdge := r.ReadBits(1)
		numEdges := uint64(1)
		if singleEdge != 1 {
			numEdges = readUnsigned(r)
		}

		pos = r.Tell()
		bsearch(int(numEdges), func(i int) int {
			seekTo := pos + int64(i)*int64(d.cbits+d.wbits+d.abits)
			if i > 0 {
				seekTo -= d.wbits
			}

			r.Seek(seekTo, 0)
			ch := rune(r.ReadBits(d.cbits))
			if ch == eStart.ch {
				if i > 0 {
					edgeEnd.count = int(r.ReadBits(d.wbits))
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

func (d *dawg) getNode(node int) nodeResult {
	var result nodeResult
	r := newBitSeeker(d.r)
	pos := int64(node)
	if pos == 0 {
		// its the first node
		pos = d.firstNodeOffset
	}

	r.Seek(pos, 0)
	nodeFinal := r.ReadBits(1)
	singleEdge := r.ReadBits(1)
	numEdges := uint64(1)
	if singleEdge != 1 {
		numEdges = readUnsigned(r)
	}

	result.node = node
	result.final = nodeFinal == 1

	for i := uint64(0); i < numEdges; i++ {
		ch := r.ReadBits(int64(d.cbits))
		var count uint64
		if i > 0 {
			count = r.ReadBits(int64(d.wbits))
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
	return result
}

func (d *dawg) getEdges() []edgeStart {
	if d.r != nil {
		log.Panicf("Not implemented")
	}

	var edges []edgeStart
	for edge := range d.edges {
		edges = append(edges, edge)
	}

	sort.Slice(edges, func(a, b int) bool {
		if edges[a].node != edges[b].node {
			return edges[a].node < edges[b].node
		}
		return edges[a].ch < edges[b].ch
	})

	return edges
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

	wordCount := readUnsigned(r)
	fmt.Printf("[%08x] WordCount=%v\n", r.Tell()-int64(unsignedLength(wordCount)*8), wordCount)

	nodeCount := readUnsigned(r)
	fmt.Printf("[%08x] NodeCount=%v\n", r.Tell()-int64(unsignedLength(nodeCount)*8), nodeCount)
	wbits := bits.Len(uint(wordCount))

	edgeCount := readUnsigned(r)
	fmt.Printf("[%08x] EdgeCount=%v\n", r.Tell()-int64(unsignedLength(edgeCount)*8), edgeCount)

	for i := 0; i < int(nodeCount); i++ {
		at := r.Tell()
		final := r.ReadBits(1)
		singleEdge := r.ReadBits(1)
		edges := uint64(1)
		if singleEdge != 1 {
			edges = readUnsigned(r)
		}
		fmt.Printf("[%08x] Node final=%d has %d edges\n", r.Tell()-int64(unsignedLength(edges)*8)-1, final, edges)

		for j := uint64(0); j < edges; j++ {
			at = r.Tell()
			ch := r.ReadBits(int64(cbits))
			var count uint64
			if j > 0 {
				count = r.ReadBits(int64(wbits))
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
