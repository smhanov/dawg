package dawg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
)

// FindResult is the result of a lookup in the d. It
// contains both the word found, and it's index based on the
// order it was added.
type FindResult struct {
	Word  string
	Index int
}

type edgeStart struct {
	node int
	ch   rune
}

func (edge edgeStart) String() string {
	return fmt.Sprintf("(%d, '%c')", edge.node, edge.ch)
}

type edgeEnd struct {
	node  int
	count int
}

type uncheckedNode struct {
	parent int
	ch     rune
	child  int
}

// EnumFn is a method that you implement. It will be called with
// all prefixes stored in the DAWG. If final is true, the prefix
// represents a complete word that has been stored.
type EnumFn = func(index int, word []rune, final bool) EnumerationResult

// EnumerationResult is returned by the enumeration function to indicate whether
// indication should continue below this depth or to stop altogether
type EnumerationResult = int

const (
	// Continue enumerating all words with this prefix
	Continue EnumerationResult = iota

	// Skip will skip all words with this prefix
	Skip

	// Stop will immediately stop enumerating words
	Stop
)

// Finder is the interface for querying a dawg. Use either
// Builder.Finish() or Load() to obtain one.
type Finder interface {
	// Find all prefixes of the given string
	FindAllPrefixesOf(input string) []FindResult

	// Find string by given prefix
	FindByPrefix(input string) string

	// Find the index of the given string
	IndexOf(input string) int

	AtIndex(index int) (string, error)

	// Enumerate all prefixes stored in the dawg.
	Enumerate(fn EnumFn)

	// Returns the number of words
	NumAdded() int

	// Returns the number of edges
	NumEdges() int

	// Returns the number of nodes
	NumNodes() int

	// Output a human-readable description of the dawg to stdout
	Print()

	// Close the dawg that was opened with Load(). After this, it is no longer
	// accessible.
	Close() error

	// Save to a writer
	Write(w io.Writer) (int64, error)

	// Save to a file
	Save(filename string) (int64, error)
}

// Builder is the interface for creating a new Dawg. Use New() to create it.
type Builder interface {
	// Add the word to the dawg
	Add(wordIn string)

	// Returns true if the word can be added.
	CanAdd(word string) bool

	// Complete the dawg and return a Finder.
	Finish() Finder
}

const rootNode = 0

type node struct {
	final bool
	count int
	edges []edgeStart
}

// dawg represents a Directed Acyclic Word Graph
type dawg struct {
	// these are erased after we finish building
	lastWord       []rune
	nextID         int
	uncheckedNodes []uncheckedNode
	minimizedNodes map[string]int
	nodes          map[int]*node

	// if read from a file, this is set
	r    io.ReaderAt
	size int64 // size of the readerAt

	// these are kept
	finished        bool
	numAdded        int
	numNodes        int
	numEdges        int
	cbits           int64 // bits to represent character value
	abits           int64 // bits to represent node address
	wbits           int64 // bits to represent number of words / counts
	firstNodeOffset int64 // first node offset in bits in the file
	hasEmptyWord    bool
}

// New creates a new dawg
func New() Builder {
	return &dawg{
		nextID:         1,
		minimizedNodes: make(map[string]int),
		nodes: map[int]*node{
			0: {count: -1},
		},
	}
}

// CanAdd will return true if the word can be added to the d.
// Words must be added in alphabetical order.
func (d *dawg) CanAdd(word string) bool {
	return !d.finished &&
		(d.numAdded == 0 || word > string(d.lastWord))
}

// Add adds a word to the structure.
// Adding a word not in alphaetical order, or to a finished dawg will panic.
func (d *dawg) Add(wordIn string) {
	if d.numAdded > 0 && wordIn <= string(d.lastWord) {
		log.Printf("Last word=%s newword=%s", string(d.lastWord), wordIn)
		panic(errors.New("d.AddWord(): Words not in alphabetical order"))
	} else if d.finished {
		panic(errors.New("d.AddWord(): Tried to add to a finished dawg"))
	}

	word := []rune(wordIn)

	// find common prefix between word and previous word
	commonPrefix := 0
	for i := 0; i < min(len(word), len(d.lastWord)); i++ {
		if word[i] != d.lastWord[i] {
			break
		}
		commonPrefix++
	}

	// Check the uncheckedNodes for redundant nodes, proceeding from last
	// one down to the common prefix size. Then truncate the list at that
	// point.
	d.minimize(commonPrefix)

	// add the suffix, starting from the correct node mid-way through the
	// graph
	var node int
	if len(d.uncheckedNodes) == 0 {
		node = rootNode
	} else {
		node = d.uncheckedNodes[len(d.uncheckedNodes)-1].child
	}

	for _, letter := range word[commonPrefix:] {
		nextNode := d.newNode()
		d.addChild(node, letter, nextNode)
		d.uncheckedNodes = append(d.uncheckedNodes, uncheckedNode{node, letter, nextNode})
		node = nextNode
	}

	d.setFinal(node)
	d.lastWord = word
	d.numAdded++
}

// Finish will mark the dawg as complete. The dawg cannot be used for lookups
// until Finish has been called.
func (d *dawg) Finish() Finder {
	if !d.finished {
		d.finished = true

		d.minimize(0)

		d.numNodes = len(d.minimizedNodes) + 1

		// Fill in the counts
		d.calculateSkipped(rootNode)

		// no longer need the names.
		d.uncheckedNodes = nil
		d.minimizedNodes = nil
		d.lastWord = nil

		d.renumber()

		var buffer bytes.Buffer
		d.size, _ = d.Write(&buffer)
		d.r = bytes.NewReader(buffer.Bytes())
		d.nodes = nil
	}

	finder, _ := Read(d.r, 0)

	return finder
}

func (d *dawg) renumber() {
	// after minimization, nodes have been removed so there are gaps in the node IDs.
	// Renumber them all to be consecutive.
	// process them in a depth-first order so that runs of characters
	// will appear in consecutive nodes, which is more efficient for encoding.

	remap := make(map[int]int)

	var process func(id int)

	process = func(id int) {
		if _, ok := remap[id]; ok {
			return
		}

		remap[id] = len(remap)
		node := d.nodes[id]
		for _, edge := range node.edges {
			process(edge.node)
		}
	}

	process(rootNode)

	nodes := make(map[int]*node)
	for id, node := range d.nodes {
		nodes[remap[id]] = node
		for i := range node.edges {
			node.edges[i].node = remap[node.edges[i].node]
		}
	}
	d.nodes = nodes
}

// Print will print all edges to the standard output
func (d *dawg) Print() {
	DumpFile(d.r)
}

func (d *dawg) FindByPrefix(input string) string {
	d.checkFinished()

	//avoid alloc
	wordSl := make([]rune, 0, len(input)*2)

	r := newBitSeeker(d.r)
	node := rootNode

	for _, letter := range input {
		edgeEnd, _, ok := d.getEdge(&r, edgeStart{node: node, ch: letter})
		// not found
		if !ok {
			return string(wordSl)
		}

		node = edgeEnd.node
		wordSl = append(wordSl, letter)
	}

	nodeResult := d.getNode(&r, node)

	// for each edge
	for {
		iterateEdge := nodeResult.edges[0]
		nodeResult = d.getNode(&r, iterateEdge.node)
		wordSl = append(wordSl, iterateEdge.ch)

		if nodeResult.final {
			break
		}
	}

	return string(wordSl)
}

// FindAllPrefixesOf returns all items in the dawg that are a prefix of the input string.
// It will panic if the dawg is not finished.
func (d *dawg) FindAllPrefixesOf(input string) []FindResult {

	d.checkFinished()

	var results []FindResult
	skipped := 0
	final := d.hasEmptyWord
	node := rootNode
	var edgeEnd edgeEnd
	var ok bool

	r := newBitSeeker(d.r)

	// for each character of the input
	for pos, letter := range input {
		// if the node is final, add a result
		if final {
			results = append(results, FindResult{
				Word:  input[:pos],
				Index: skipped,
			})
		}

		// check if there is an outgoing edge for the letter
		edgeEnd, final, ok = d.getEdge(&r, edgeStart{node: node, ch: letter})
		if !ok {
			return results
		}

		// we found an edge.
		node = edgeEnd.node
		skipped += edgeEnd.count
	}

	if final {
		results = append(results, FindResult{
			Word:  input,
			Index: skipped,
		})
	}

	return results
}

// IndexOf returns the index, which is the order the item was inserted.
// If the item was never inserted, it returns -1
// It will panic if the dawg is not finished.
func (d *dawg) IndexOf(input string) int {
	skipped := 0
	node := rootNode
	final := d.hasEmptyWord
	var ok bool
	var edgeEnd edgeEnd
	r := newBitSeeker(d.r)

	// for each character of the input
	for _, letter := range input {
		// check if there is an outgoing edge for the letter
		edgeEnd, final, ok = d.getEdge(&r, edgeStart{node: node, ch: letter})
		//log.Printf("Follow %v:%v=>%v (ok=%v)", node, string(letter), edgeEnd.node, ok)
		if !ok {
			// not found
			return -1
		}

		// we found an edge.
		node = edgeEnd.node
		skipped += edgeEnd.count
	}

	//log.Printf("IsFinal %d: %v", node, final)
	if final {
		return skipped
	}
	return -1
}

// NumAdded returns the number of words added
func (d *dawg) NumAdded() int {
	return d.numAdded
}

// NumNodes returns the number of nodes in the d.
func (d *dawg) NumNodes() int {
	return d.numNodes
}

// NumEdges returns the number of edges in the d. This includes transitions to
// the "final" node after each word.
func (d *dawg) NumEdges() int {
	return d.numEdges
}

func (d *dawg) checkFinished() {
	if !d.finished {
		panic(errors.New("dawg was not Finished()"))
	}
}

func (d *dawg) minimize(downTo int) {
	// proceed from the leaf up to a certain point
	for i := len(d.uncheckedNodes) - 1; i >= downTo; i-- {
		u := d.uncheckedNodes[i]
		name := d.nameOf(u.child)
		if node, ok := d.minimizedNodes[name]; ok {
			// replace the child with the previously encountered one
			d.replaceChild(u.parent, u.ch, node)
		} else {
			// add the state to the minimized nodes.
			d.minimizedNodes[name] = u.child
		}
	}

	d.uncheckedNodes = d.uncheckedNodes[:downTo]
}

func (d *dawg) newNode() int {
	d.nextID++
	return d.nextID - 1
}

func (d *dawg) nameOf(nodeid int) string {
	node := d.nodes[nodeid]

	// node name is id_ch:id... for each child
	buff := bytes.Buffer{}
	for _, edge := range node.edges {
		buff.WriteByte('_')
		buff.WriteRune(edge.ch)
		buff.WriteByte(':')
		buff.WriteString(strconv.Itoa(edge.node))
	}

	if node.final {
		buff.WriteByte('!')
	}

	return buff.String()
}

func (d *dawg) setFinal(node int) {
	d.nodes[node].final = true
	if node == rootNode {
		d.hasEmptyWord = true
	}
}

func (d *dawg) addChild(parent int, ch rune, child int) {
	//log.Printf("Addchild %v(%v)->%v", parent, string(ch), child)
	d.numEdges++
	if d.nodes[child] == nil {
		d.nodes[child] = &node{
			count: -1,
		}
	}
	node := d.nodes[parent]
	if len(node.edges) > 0 && ch <= node.edges[len(node.edges)-1].ch {
		log.Panic("Not strictly increasing")
	}
	node.edges = append(node.edges, edgeStart{child, ch})
}

func (d *dawg) replaceChild(parent int, ch rune, child int) {
	pnode := d.nodes[parent]
	//TODO: should be bsearch
	i := bsearch(len(pnode.edges), func(i int) int {
		return int(pnode.edges[i].ch - ch)
	})

	if pnode.edges[i].ch != ch {
		//for _, edge := range pnode.edges {
		//	log.Printf("Edge %c %d", rune(edge.ch), edge.node)
		//}
		log.Panicf("Not found: %c", ch)
	}

	//log.Printf("ReplaceChild(%v:%v=>%v, %v:%v=>%v)",
	//	parent, string(ch), pnode.edges[i].node,
	//	parent, string(ch), child)

	delete(d.nodes, pnode.edges[i].node)
	pnode.edges[i].node = child

}

func (d *dawg) calculateSkipped(nodeid int) int {
	// for each child of the node, calculate now many nodes
	// are skipped over by following that child. This is the
	// sum of all skipped-over counts of its previous siblings.

	// returns the number of leaves reachable from the node.
	node := d.nodes[nodeid]
	if node.count >= 0 {
		return node.count
	}

	numReachable := 0

	if node.final {
		numReachable++
	}

	for _, edge := range node.edges {
		numReachable += d.calculateSkipped(edge.node)
	}

	node.count = numReachable

	return numReachable
}

// Enumerate will call the given method, passing it every possible prefix of words in the index.
// Return Continue to continue enumeration, Skip to skip this branch, or Stop to stop enumeration.
func (d *dawg) Enumerate(fn EnumFn) {
	r := newBitSeeker(d.r)
	d.enumerate(&r, 0, rootNode, nil, fn)
}

func (d *dawg) enumerate(r *bitSeeker, index int, address int, runes []rune, fn EnumFn) EnumerationResult {
	// get the node and whether its final
	node := d.getNode(r, address)

	// call the enum function on the runes
	result := fn(index, runes, node.final)

	// if the function didn't say to continue, then return.
	if result != Continue {
		return result
	}

	l := len(runes)
	runes = append(runes, 0)

	// for each edge
	for _, edge := range node.edges {
		// add ch to the runes
		runes[l] = edge.ch
		// recurse
		result = d.enumerate(r, index+edge.count, edge.node, runes, fn)
		if result == Stop {
			break
		}
	}

	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (d *dawg) AtIndex(index int) (string, error) {
	if index < 0 || index >= d.NumAdded() {
		return "", errors.New("invalid index")
	}

	r := newBitSeeker(d.r)
	// start at first node and empty string
	result, _ := d.atIndex(&r, rootNode, 0, index, nil)
	return result, nil
}

func (d *dawg) atIndex(r *bitSeeker, nodeNumber, atIndex, targetIndex int, runes []rune) (string, bool) {
	node := d.getNode(r, nodeNumber)
	// if node is final and index matches, return it
	if node.final && atIndex == targetIndex {
		return string(runes), true
	}

	next := bsearch(len(node.edges), func(i int) int {
		return atIndex + node.edges[i].count - targetIndex
	})

	if next == len(node.edges) || atIndex+node.edges[next].count > targetIndex {
		next--
	}

	//log.Printf("Follow edge %v %c skip=%d", node.edges[next], node.edges[next].ch, node.edges[next].count)
	runes = append(runes, 0)
	for i := next; i < len(node.edges); i++ {
		runes[len(runes)-1] = node.edges[i].ch
		if result, ok := d.atIndex(r, node.edges[i].node, atIndex+node.edges[i].count, targetIndex, runes); ok {
			return result, ok
		}
	}
	return "", false

}
