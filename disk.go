package dawg

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
)

func edgeHash(d int32, edge edgeStart) int {
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

type readerAt struct {
	io.ReaderAt
}

func (r *readerAt) readUint32(at int64) uint32 {
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

func (r *readerAt) readInt32(at int64) int {
	val := int32(r.readUint32(at))
	return int(val)
}

// Save writes the DAWG to an io.Writer. Returns the number of bytes written
func (dawg *Dawg) Write(w io.Writer) (int, error) {
	dawg.Finish()

	// get all the edges
	edges := dawg.getEdges()
	numEdges := len(edges)

	//log.Printf("Edges are %v", edges)

	// create minimal perfect hash
	G, permute := CreateMinimalPerfectHash(len(edges), func(d int32, i int) int {
		return edgeHash(d, edges[i])
	})

	// format:
	// uint32 -- number of bytes that will be written
	// uint32 -- number of words
	// uint32 -- number of nodes
	// uint32 -- number of edges
	// for each edge:
	//    uint32 Hash displacement value
	//    uint32 edgeStart node number
	//    uint32 edgeStart character
	//    uint32 edgeEnd node number
	//    uint32 edgeEnd count

	size := edgesOffset + numEdges*edgeRecordLength

	//log.Printf("Write %d edges to disk %d", numEdges, len(permute))

	//log.Printf("Permute: %v", permute)

	writeInt(w, size)
	writeInt(w, dawg.NumAdded())
	writeInt(w, dawg.NumNodes())
	writeInt(w, dawg.NumEdges())
	for i, src := range permute {
		start := edges[src]
		end := dawg.edges[start]
		//log.Printf("Write edge %v: d=%v %v %v", i, G[i], start, end)
		writeInt32(w, G[i])
		writeInt32(w, int32(start.node))
		writeInt32(w, int32(start.ch))
		writeInt32(w, int32(end.node))
		writeInt32(w, int32(end.count))
	}

	return size, nil
}

// Load loads the dawg from a file
func Load(filename string) (Finder, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return Read(&readerAt{f}, 0)
}

const edgesOffset = 4 * 4
const edgeRecordLength = 4 * 5

// Read returns a finder that accesses the dawg in-place using the
// given io.ReaderAt
func Read(f io.ReaderAt, offset int64) (Finder, error) {
	r := &readerAt{f}
	size := r.readUint32(offset)
	//log.Printf("Dawg is %v bytes;", size)
	r = &readerAt{io.NewSectionReader(f, offset, int64(size))}

	dawg := &Dawg{
		finished: true,
		numAdded: int(r.readUint32(4)),
		numNodes: int(r.readUint32(8)),
		numEdges: int(r.readUint32(12)),
		r:        r,
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
		var index int64
		hash1 := edgeHash(0, eStart) % dawg.numEdges
		d := int(dawg.r.readInt32(edgesOffset + int64(hash1)*edgeRecordLength))
		if d < 0 {
			index = int64(-d - 1)
		} else {
			index = int64(edgeHash(int32(d), eStart) % dawg.numEdges)
		}
		//log.Printf("edge hash1=%d d=%d hash2=%d numEdges=%d", hash1, d, index, dawg.numEdges)

		if index < 0 || index >= int64(dawg.numEdges) {
			log.Panic(errors.New("Invalid index from hash"))
		}

		record := int64(edgesOffset + index*edgeRecordLength)

		var key edgeStart
		key.node = int(dawg.r.readUint32(record + 4))
		key.ch = rune(dawg.r.readUint32(record + 8))
		//log.Printf("Read key %v, wanted=%v", key, eStart)

		if eStart == key {
			edgeEnd.node = int(dawg.r.readUint32(record + 12))
			edgeEnd.count = int(dawg.r.readUint32(record + 16))
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
		var i int64
		for i = 0; i < int64(dawg.numEdges); i++ {
			var key edgeStart
			record := int64(edgesOffset + i*edgeRecordLength)
			//log.Printf("Read edge #%d offset %d", i, record)
			key.node = int(dawg.r.readUint32(record + 4))
			key.ch = rune(dawg.r.readUint32(record + 8))
			edges = append(edges, key)
		}
	}

	sort.Slice(edges, func(a, b int) bool {
		return edges[a].node < edges[b].node
	})

	return edges
}
