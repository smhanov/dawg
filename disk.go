package dawg

import (
	"fmt"
	"io"
	"log"
	"math/bits"
	"os"
	"sort"
)

func edgeHash(d uint32, edge edgeStart) uint32 {
	return StringHash(d, fmt.Sprintf("%d:%d", edge.node, edge.ch))
}

// Save writes the DAWG to disk. Returns the number of bytes written
func (dawg *Dawg) Save(filename string) (int, error) {
	dawg.checkFinished()

	f, err := os.Create(filename)
	if err != nil {
		return 0, err
	}

	defer f.Close()
	return dawg.Write(f)
}

func writeInt32(w io.Writer, vi int32) error {
	v := uint32(vi)
	_, err := w.Write([]byte{
		byte((v >> 24) & 0xff),
		byte((v >> 16) & 0xff),
		byte((v >> 8) & 0xff),
		byte((v) & 0xff),
	})

	if err != nil {
		log.Panic(err)
	}

	return err
}

func writeInt(w io.Writer, vi int) error {
	return writeInt32(w, int32(vi))
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

// Save writes the DAWG to an io.Writer. Returns the number of bytes written
func (dawg *Dawg) Write(wIn io.Writer) (int, error) {
	w := NewBitWriter(wIn)
	dawg.Finish()

	// get all the edges
	edges := dawg.getEdges()
	numEdges := len(edges)

	next := make([]rune, numEdges, numEdges)

	// for every edge, find the character of its next sibling.
	for i := 1; i < numEdges; i++ {
		if edges[i-1].node == edges[i].node {
			next[i-1] = edges[i].ch
		}
	}

	//log.Printf("Edges are %v", edges)

	// create minimal perfect hash
	G, permute := CreateMinimalPerfectHash(len(edges), func(d uint32, i int) uint32 {
		return edgeHash(d, edges[i])
	})

	// format:
	// uint32 -- number of bytes that will be written
	// uint32 -- number of words
	// uint32 -- number of nodes
	// uint32 -- number of edges
	// let ebits = log2(#edges)
	// let chbits = log2(max character)
	// let nbits = log2(#nodes)
	// let hbits = 1 + log2(highest hash value)
	// let wbits = log2(#words)
	// let edgeSize = 1+hbits+nbits+chbits+nbits+nbits
	// for each edge:
	//    hbits Hash displacement value
	//    nbits edgeStart node number
	//    chbits edgeStart character
	//    nbits edgeEnd node number
	//    wbits edgeEnd count

	nbits := bits.Len(uint(dawg.NumNodes()))

	highestHash := 0
	for _, g := range G {
		if g < 0 {
			g = -g - 1
		}
		if int(g) > highestHash {
			highestHash = int(g)
		}
	}
	hbits := bits.Len(uint(highestHash))

	highestChar := 0
	for _, edge := range edges {
		if int(edge.ch) > highestChar {
			highestChar = int(edge.ch)
		}
	}

	cbits := bits.Len(uint(highestChar))
	wbits := int(bits.Len(uint(dawg.NumAdded())))
	edgeRecordLength := 1 + hbits + nbits + cbits + nbits + wbits

	size := edgesOffset + (numEdges*edgeRecordLength+7)/8

	//log.Printf("Write %d edges to disk %d", numEdges, len(permute))

	//log.Printf("Permute: %v", permute)

	w.WriteBits(uint64(size), 32)
	w.WriteBits(uint64(dawg.NumAdded()), 32)
	w.WriteBits(uint64(dawg.NumNodes()), 32)
	w.WriteBits(uint64(dawg.NumEdges()), 32)
	w.WriteBits(uint64(hbits), 8)
	w.WriteBits(uint64(cbits), 8)

	// for each node, is it final?
	for i := 0; i < dawg.NumNodes(); i++ {
		if dawg.isFinal(i) {
			w.WriteBits(1, 1)
		} else {
			w.WriteBits(0, 1)
		}
	}

	for i, src := range permute {
		start := edges[src]
		end := dawg.edges[start]
		//log.Printf("Write edge %v: d=%v %v %v", i, G[i], start, end)
		if G[i] < 0 {
			w.WriteBits(1, 1)
			w.WriteBits(uint64(-G[i]-1), hbits)
		} else {
			w.WriteBits(0, 1)
			w.WriteBits(uint64(G[i]), hbits)
		}
		w.WriteBits(uint64(start.node), nbits)
		w.WriteBits(uint64(start.ch), cbits)
		w.WriteBits(uint64(end.node), nbits)
		w.WriteBits(uint64(end.count), wbits)
		w.WriteBits(uint64(next[src]), cbits)
	}

	w.Flush()

	return size, nil
}

// Load loads the dawg from a file
func Load(filename string) (Finder, error) {
	f, err := os.Open(filename)
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

	r := NewBitSeeker(io.NewSectionReader(f, offset, int64(size)))

	r.Seek(32, 0)
	numAdded := int(r.ReadBits(32))
	numNodes := int(r.ReadBits(32))
	numEdges := int(r.ReadBits(32))
	hbits := int64(r.ReadBits(8))
	cbits := int64(r.ReadBits(8))
	wbits := int64(bits.Len(uint(numAdded)))
	nbits := int64(bits.Len(uint(numNodes)))
	dawg := &Dawg{
		finished:         true,
		numAdded:         numAdded,
		numNodes:         numNodes,
		numEdges:         numEdges,
		nbits:            nbits,
		cbits:            cbits,
		hbits:            hbits,
		wbits:            wbits,
		edgeRecordLength: int64(1 + hbits + nbits + cbits + nbits + wbits + cbits),
		r:                f,
	}

	//log.Printf("Frozen dawg has %d edges", dawg.numEdges)

	return dawg, nil
}

func (dawg *Dawg) getEdge(eStart edgeStart) (edgeEnd, bool) {
	var edgeEnd edgeEnd
	var ok bool
	if dawg.numEdges == 0 {
		// do nothing
	} else if dawg.r == nil {
		edgeEnd, ok = dawg.edges[eStart]
	} else {
		r := NewBitSeeker(dawg.r)
		var index int64
		hash1 := int64(edgeHash(0, eStart) % uint32(dawg.numEdges))
		r.Seek(edgesOffset+int64(dawg.numNodes)+hash1*dawg.edgeRecordLength, 0)
		neg := r.ReadBits(1)
		d := int64(r.ReadBits(dawg.hbits))
		if neg == 1 {
			index = d
		} else {
			index = int64(edgeHash(uint32(d), eStart) % uint32(dawg.numEdges))
		}
		//log.Printf("edge hash1=%d neg=%d d=%d hash2=%d numEdges=%d raw=%d", hash1, neg, d, index, dawg.numEdges,
		//	edgeHash(uint32(d), eStart))

		if index < 0 || index >= int64(dawg.numEdges) {
			log.Panicf("Invalid index from hash: %d", index)

		}

		r.Seek(edgesOffset+int64(dawg.numNodes)+index*int64(dawg.edgeRecordLength)+1+dawg.hbits, 0)

		var key edgeStart
		key.node = int(r.ReadBits(dawg.nbits))
		key.ch = rune(r.ReadBits(dawg.cbits))
		//log.Printf("Read key %v, wanted=%v", key, eStart)

		if eStart == key {
			edgeEnd.node = int(r.ReadBits(dawg.nbits))
			edgeEnd.count = int(r.ReadBits(dawg.wbits))
			ok = true
		}
	}

	//log.Printf("getEdge(%v) returning %v,%v", eStart, edgeEnd, ok)

	return edgeEnd, ok
}

func (dawg *Dawg) getEdges() []edgeStart {
	var edges []edgeStart
	if dawg.r == nil {
		for edge := range dawg.edges {
			edges = append(edges, edge)
		}
		//log.Printf("There are %d edges %d", len(dawg.edges), dawg.numEdges)
	} else {
		r := NewBitSeeker(dawg.r)
		r.Seek(edgesOffset+int64(dawg.numNodes), 0)
		var i int
		for i = 0; i < dawg.numEdges; i++ {
			var key edgeStart
			r.Skip(1 + int64(dawg.hbits))
			key.node = int(r.ReadBits(dawg.nbits))
			key.ch = rune(r.ReadBits(dawg.cbits))
			r.Skip(dawg.nbits + dawg.wbits + dawg.cbits)
			edges = append(edges, key)
		}
	}

	sort.Slice(edges, func(a, b int) bool {
		return edges[a].node < edges[b].node
	})

	return edges
}

// DumpFile prints out the file
func DumpFile(f io.ReaderAt) {
	r := NewBitSeeker(f)
	size := r.ReadBits(32)
	fmt.Printf("[%08x] Size=%v bytes\n", r.Tell()-32, size)

	wordCount := r.ReadBits(32)
	fmt.Printf("[%08x] WordCount=%v\n", r.Tell()-32, wordCount)

	nodeCount := r.ReadBits(32)
	fmt.Printf("[%08x] NodeCount=%v\n", r.Tell()-32, nodeCount)

	edgeCount := r.ReadBits(32)
	fmt.Printf("[%08x] EdgeCount=%v\n", r.Tell()-32, edgeCount)

	hbits := int64(r.ReadBits(8))
	fmt.Printf("[%08x] HashBits=%v\n", r.Tell()-8, hbits)

	cbits := int64(r.ReadBits(8))
	fmt.Printf("[%08x] RuneBits=%v\n", r.Tell()-8, cbits)

	nbits := int64(bits.Len(uint(nodeCount)))
	wbits := int64(bits.Len(uint(wordCount)))

	for i := 0; i < int(nodeCount); i++ {
		fmt.Printf("[%08x] Node %d final: %d\n", r.Tell(), i, r.ReadBits(1))
	}

	fmt.Printf("1 hbits:%d nbits:%d cbits:%d nbits:%d wbits:%d\n",
		hbits, nbits, cbits, nbits, wbits)

	for i := 0; i < int(edgeCount); i++ {
		at := r.Tell()
		neg := r.ReadBits(1)
		d := r.ReadBits(hbits)

		startNode := r.ReadBits(nbits)
		ch := r.ReadBits(cbits)
		endNode := r.ReadBits(nbits)
		count := r.ReadBits(wbits)
		next := r.ReadBits(cbits)

		var hash string
		if neg > 0 {
			hash = fmt.Sprintf("Hash=%06d (D)", d)
		} else {
			hash = fmt.Sprintf("Hash=%06d    ", d)
		}

		fmt.Printf("[%08x] %s start=%06d:%c goto %06d skipping %d next=%c\n",
			at, hash, startNode, rune(ch), endNode, count, next)

	}
}

func writeUnsigned(w BitWriter, n uint64) {
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

func readUnsigned(r BitSeeker) uint64 {
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

func encodeCharset(w BitWriter, chars map[rune]bool) {
	// get a list of runes
	runes := make([]rune, 0, len(chars))
	for r := range chars {
		runes = append(runes, r)
	}

	// sort them
	sort.Slice(runes, func(i, j int) bool {
		return runes[i] < runes[j]
	})

	log.Printf("Sorted runes: %v", runes)

	// encode as differences
	for i := len(runes) - 1; i >= 1; i-- {
		runes[i] -= runes[i-1]
	}

	log.Printf("Rune diffs: %v", runes)

	// run-length encode
	last := rune(0x7fffffff)
	run := uint64(0)
	for _, r := range runes {
		if r != last {
			if run > 0 {
				writeUnsigned(w, run)
				writeUnsigned(w, uint64(last))
			}
			run = 1
			last = r
		} else {
			run++
		}
	}

	if run > 0 {
		writeUnsigned(w, run)
		writeUnsigned(w, uint64(last))
	}

	// end with 0
	writeUnsigned(w, 0)
}

func decodeCharset(r BitSeeker) map[uint64]rune {
	result := make(map[uint64]rune)
	var runes []rune

	for {
		run := readUnsigned(r)
		if run == 0 {
			break
		}
		ch := readUnsigned(r)

		for i := uint64(0); i < run; i++ {
			runes = append(runes, rune(ch))
		}
	}

	// decode differences
	for i := 1; i < len(runes); i++ {
		runes[i] += runes[i-1]
	}

	for i, r := range runes {
		result[uint64(i)] = r
	}

	return result
}
