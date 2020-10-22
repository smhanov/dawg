package dawg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
)

// FindResult is the result of a lookup in the Dawg. It
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

// EnumFn is a method to enumerate
type EnumFn = func(word rune[], final bool) bool

// Finder is the interface for using a Dawg to find words
// A dawg stored on disk only implements this interface, since it
// cannot be added to.
type Finder interface {
	FindAllPrefixesOf(input string) []FindResult
	IndexOf(input string) int
	Enumerate(fn EnumFn)
	NumAdded() int
	NumEdges() int
	NumNodes() int
	Print()
}

// Builder is the interface for creating a new Dawg
type Builder interface {
	CanAdd(word string) bool
	Add(wordIn string)
	Finish() Finder
	Write(w io.Writer) (int, error)
	Save(filename string) (int, error)
}

const rootNode = 0

// Dawg represents a Directed Acyclic Word Graph
type Dawg struct {
	// these are erased after we finish building
	lastWord       []rune
	nextID         int
	uncheckedNodes []uncheckedNode
	minimizedNodes map[string]int
	names          map[int][]edgeStart

	// if read from a file, this is set
	r io.ReaderAt

	// these are kept
	finished         bool
	numAdded         int
	numNodes         int
	numEdges         int
	hbits            int64 // bits to represent hash value
	cbits            int64 // bits to represent character value
	nbits            int64 // bits to represent node value
	wbits            int64 // bits to represent number of words / counts
	edgeRecordLength int64
	edges            map[edgeStart]edgeEnd
	final            map[int]bool // is node final?
}

// New creates a new DAWG
func New() Builder {
	return &Dawg{
		nextID:         1,
		minimizedNodes: make(map[string]int),
		names:          make(map[int][]edgeStart),
		edges:          make(map[edgeStart]edgeEnd),
		final:          make(map[int]bool),
	}
}

// CanAdd will return true if the word can be added to the Dawg.
// Words must be added in alphabetical order.
func (dawg *Dawg) CanAdd(word string) bool {
	return !dawg.finished &&
		(dawg.numAdded == 0 || word > string(dawg.lastWord))
}

// Add adds a word to the structure.
// Adding a word not in alphaetical order, or to a finished Dawg will panic.
func (dawg *Dawg) Add(wordIn string) {
	if dawg.numAdded > 0 && wordIn <= string(dawg.lastWord) {
		log.Printf("Last word=%s newword=%s", string(dawg.lastWord), wordIn)
		panic(errors.New("Dawg.AddWord(): Words not in alphabetical order"))
	} else if dawg.finished {
		panic(errors.New("Dawg.AddWord(): Tried to add to a finished Dawg"))
	}

	word := []rune(wordIn)

	// find common prefix between word and previous word
	commonPrefix := 0
	for i := 0; i < min(len(word), len(dawg.lastWord)); i++ {
		if word[i] != dawg.lastWord[i] {
			break
		}
		commonPrefix++
	}

	// Check the uncheckedNodes for redundant nodes, proceeding from last
	// one down to the common prefix size. Then truncate the list at that
	// point.
	dawg.minimize(commonPrefix)

	// add the suffix, starting from the correct node mid-way through the
	// graph
	var node int
	if len(dawg.uncheckedNodes) == 0 {
		node = rootNode
	} else {
		node = dawg.uncheckedNodes[len(dawg.uncheckedNodes)-1].child
	}

	for _, letter := range word[commonPrefix:] {
		nextNode := dawg.newNode()
		dawg.addChild(node, letter, nextNode)
		dawg.uncheckedNodes = append(dawg.uncheckedNodes, uncheckedNode{node, letter, nextNode})
		node = nextNode
	}

	dawg.setFinal(node)
	dawg.lastWord = word
	dawg.numAdded++
}

// Finish will mark the dawg as complete. The Dawg cannot be used for lookups
// until Finish has been called.
func (dawg *Dawg) Finish() Finder {
	if dawg.finished {
		return dawg
	}
	dawg.finished = true

	dawg.minimize(0)
	/*log.Printf("After minimizing %v", dawg.minimizedNodes)
	for name, node := range dawg.minimizedNodes {
		log.Printf("%v=>%v\n", name, node)
	}
	for _, node := range dawg.uncheckedNodes {
		log.Printf("%v\n", node)
	}*/

	dawg.numNodes = len(dawg.minimizedNodes) + 1
	dawg.numEdges = len(dawg.edges)

	// Fill in the counts
	cache := make(map[int]int)
	dawg.calculateSkipped(cache, rootNode)

	// no longer need the names.
	dawg.names = nil
	dawg.uncheckedNodes = nil
	dawg.minimizedNodes = nil
	dawg.lastWord = nil

	dawg.renumber()

	log.Printf("After finish, there are %d edges", dawg.numEdges)
	return dawg
}

func (dawg *Dawg) renumber() {
	// after minimization, nodes have been removed so there are gaps in the node IDs.
	// Renumber them all to be consecutive.

	remap := make(map[int]int)
	remap[rootNode] = rootNode

	for start, end := range dawg.edges {
		if _, ok := remap[start.node]; !ok {
			remap[start.node] = len(remap)
		}
		if _, ok := remap[end.node]; !ok {
			remap[end.node] = len(remap)
		}
	}

	edges := make(map[edgeStart]edgeEnd)
	for start, end := range dawg.edges {
		edges[edgeStart{remap[start.node], start.ch}] = edgeEnd{remap[end.node], end.count}
	}
	dawg.edges = edges

	final := make(map[int]bool)
	for node, isFinal := range dawg.final {
		final[remap[node]] = isFinal
	}

	dawg.final = final
}

// Print will print all edges to the standard output
func (dawg *Dawg) Print() {
	var edgeEnd edgeEnd
	fmt.Printf("Dawg has %v words, %v edges\n", dawg.numAdded, dawg.numEdges)
	for _, edgeStart := range dawg.getEdges() {
		edgeEnd, _ = dawg.getEdge(edgeStart)
		if dawg.isFinal(edgeEnd.node) {
			fmt.Printf("%v:%v goto %v skipped=%v (Final)\n", edgeStart.node, string(edgeStart.ch), edgeEnd.node, edgeEnd.count)
		} else {
			fmt.Printf("%v:%v goto %v skipped=%v\n", edgeStart.node, string(edgeStart.ch), edgeEnd.node, edgeEnd.count)
		}
	}
}

// FindAllPrefixesOf returns all items in the dawg that are a prefix of the input string.
// It will panic if the dawg is not finished.
func (dawg *Dawg) FindAllPrefixesOf(input string) []FindResult {

	dawg.checkFinished()

	var results []FindResult
	skipped := 0
	node := rootNode

	// for each character of the input
	for pos, letter := range input {
		// if the node is final, add a result
		if dawg.isFinal(node) {
			log.Printf("node %v is final", node)
			results = append(results, FindResult{
				Word:  input[:pos],
				Index: skipped,
			})
		}

		// check if there is an outgoing edge for the letter
		edgeEnd, ok := dawg.getEdge(edgeStart{node: node, ch: letter})
		log.Printf("Follow %v:%v=>%v (ok=%v)", node, string(letter), edgeEnd.node, ok)
		if !ok {
			return results
		}

		// we found an edge.
		node = edgeEnd.node
		skipped += edgeEnd.count
	}

	if dawg.isFinal(node) {
		log.Printf("node %v is final", node)
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
func (dawg *Dawg) IndexOf(input string) int {
	skipped := 0
	node := rootNode

	// for each character of the input
	for _, letter := range input {
		// check if there is an outgoing edge for the letter
		edgeEnd, ok := dawg.getEdge(edgeStart{node: node, ch: letter})
		//log.Printf("Follow %v:%v=>%v (ok=%v)", node, string(letter), edgeEnd.node, ok)
		if !ok {
			// not found
			return -1
		}

		// we found an edge.
		node = edgeEnd.node
		skipped += edgeEnd.count
	}

	//log.Printf("IsFinal %d: %v", node, dawg.isFinal(node))
	if dawg.isFinal(node) {
		return skipped
	}
	return -1
}

// NumAdded returns the number of words added
func (dawg *Dawg) NumAdded() int {
	return dawg.numAdded
}

// NumNodes returns the number of nodes in the dawg.
func (dawg *Dawg) NumNodes() int {
	return dawg.numNodes
}

// NumEdges returns the number of edges in the dawg. This includes transitions to
// the "final" node after each word.
func (dawg *Dawg) NumEdges() int {
	return dawg.numEdges
}

func (dawg *Dawg) checkFinished() {
	if !dawg.finished {
		panic(errors.New("DAWG was not Finished()"))
	}
}

func (dawg *Dawg) minimize(downTo int) {
	// proceed from the leaf up to a certain point
	for i := len(dawg.uncheckedNodes) - 1; i >= downTo; i-- {
		u := dawg.uncheckedNodes[i]
		name := dawg.nameOf(u.child)
		if node, ok := dawg.minimizedNodes[name]; ok {
			// replace the child with the previously encountered one
			dawg.replaceChild(u.parent, u.ch, node)
		} else {
			// add the state to the minimized nodes.
			dawg.minimizedNodes[name] = u.child
		}
	}

	dawg.uncheckedNodes = dawg.uncheckedNodes[:downTo]
}

func (dawg *Dawg) newNode() int {
	dawg.nextID++
	return dawg.nextID - 1
}

func (dawg *Dawg) nameOf(node int) string {
	// node name is id_ch:id... for each child
	buff := bytes.Buffer{}
	for _, edge := range dawg.names[node] {
		buff.WriteByte('_')
		buff.WriteRune(edge.ch)
		buff.WriteByte(':')
		buff.WriteString(strconv.Itoa(edge.node))
	}

	if dawg.isFinal(node) {
		buff.WriteByte('!')
	}

	return buff.String()
}

func (dawg *Dawg) setFinal(node int) {
	dawg.final[node] = true
}

func (dawg *Dawg) isFinal(node int) bool {
	if dawg.r == nil {
		return dawg.final[node]
	}

	r := NewBitSeeker(dawg.r)
	r.Seek(edgesOffset+int64(node), 0)
	return r.ReadBits(1) == 1
}

func (dawg *Dawg) addChild(parent int, ch rune, child int) {
	//log.Printf("Addchild %v(%v)->%v", parent, string(ch), child)
	dawg.names[parent] = append(dawg.names[parent], edgeStart{child, ch})
	dawg.edges[edgeStart{parent, ch}] = edgeEnd{node: child}
}

func (dawg *Dawg) getChild(parent int, ch rune) edgeEnd {
	return dawg.edges[edgeStart{parent, ch}]
}

func (dawg *Dawg) replaceChild(parent int, ch rune, child int) {
	start := edgeStart{parent, ch}
	oldChild := dawg.edges[start].node

	//log.Printf("ReplaceChild(%v:%v=>%v, %v:%v=>%v)",
	//	parent, string(ch), oldChild,
	//	parent, string(ch), child)

	// remove all edges out of the old child to save memory
	for _, eStart := range dawg.names[oldChild] {
		//log.Printf("Remove old link %v:%v=>%v", oldChild, string(eStart.ch), eStart.node)
		link := edgeStart{node: oldChild, ch: eStart.ch}
		delete(dawg.edges, link)
	}

	delete(dawg.names, oldChild)
	delete(dawg.final, oldChild)

	// go through the names info of the parent and replace the item
	name := dawg.names[parent]
	for i := range name {
		if name[i].ch == ch {
			name[i].node = child
			break
		}
	}

	// finally, set the edge of the parent
	dawg.edges[start] = edgeEnd{node: child}
}

func (dawg *Dawg) calculateSkipped(cache map[int]int, node int) int {
	// for each child of the node, calculate now many nodes
	// are skipped over by following that child. This is the
	// sum of all skipped-over counts of its previous siblings.

	// returns the number of leaves reachable from the node.
	if count, ok := cache[node]; ok {
		return count
	}

	//log.Printf("Processing %s", dawg.nameOf(node))
	edges := dawg.names[node]

	numReachable := 0

	if dawg.isFinal(node) {
		numReachable++
	}

	for _, eStart := range edges {
		// if it marks the final node, then add one
		dawg.setCount(node, eStart.ch, numReachable)
		numReachable += dawg.calculateSkipped(cache, eStart.node)
	}

	cache[node] = numReachable
	return numReachable
}

func (dawg *Dawg) Enumerate(fn EnumFn) {
	// let nextCh = first edge from root's character
	// let runes be empty

	// enumerate from the root, nextch
}

func (dawg *Dawg) enumerate(runes []rune, ch rune, fn EnumFn) {
	for {
		// follow the edge, getting nextCh

		// add ch to runes and call the function

		// if function returned true, then 

		// call the function
}

func (dawg *Dawg) setCount(node int, ch rune, count int) {
	start := edgeStart{node: node, ch: ch}
	end := dawg.edges[start]
	end.count = count
	dawg.edges[start] = end
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
