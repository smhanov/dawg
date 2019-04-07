package dawg

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
)

// FindResult is the result of a lookup in the Dawg. It
// contains both the word found, and it's index based on the
// order it was added.
type FindResult struct {
	Word  string
	Index int
}

// Dawg represents a Directed Acyclic Word Graph
type Dawg interface {
	// CanAdd will return true if the word can be added to the Dawg.
	// Words must be added in alphabetical order.
	CanAdd(word string) bool

	// Adding a word not in alphaetical order, or to a finished Dawg will panic.
	Add(word string)

	// Finish will mark the dawg as complete
	Finish()

	// FindAll returns all items in the dawg that are a prefix of the input string.
	// It will panic if the dawg is not finished.
	FindAllPrefixesOf(input string) []FindResult

	// IndexOf returns the index, which is the order the item was inserted.
	// If the item was never inserted, it returns -1
	// It will panic if the dawg is not finished.
	IndexOf(input string) int

	// NumAdded returns the number of words added
	NumAdded() int

	// NumNodes returns the number of nodes in the dawg.
	NumNodes() int

	// NumEdges returns the number of edges in the dawg. This includes transitions to
	// the "final" node after each word.
	NumEdges() int

	// Print ...
	Print()
}

type edgeStart struct {
	ch   rune
	node int
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

type dawg struct {
	// these are erased after we finish building
	lastWord       []rune
	nextID         int
	uncheckedNodes []uncheckedNode
	minimizedNodes map[string]int
	names          map[int][]edgeStart

	// these are kept
	finished bool
	root     int
	numNodes int
	numAdded int
	edges    map[edgeStart]edgeEnd
}

// NewDAWG creates a new DAWG
func NewDAWG() Dawg {
	return &dawg{
		nextID:         1,
		minimizedNodes: make(map[string]int),
		names:          make(map[int][]edgeStart),
		edges:          make(map[edgeStart]edgeEnd),
	}
}

func (dawg *dawg) CanAdd(word string) bool {
	return !dawg.finished &&
		(dawg.numAdded == 0 || word > string(dawg.lastWord))
}

func (dawg *dawg) Add(wordIn string) {
	if dawg.numAdded > 0 && wordIn <= string(dawg.lastWord) {
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
		node = dawg.root
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

func (dawg *dawg) Finish() {
	if dawg.finished {
		return
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

	dawg.numNodes = len(dawg.minimizedNodes)

	// Fill in the counts
	cache := make(map[int]int)
	dawg.calculateSkipped(cache, dawg.root)

	// no longer need the names.
	dawg.names = nil
	dawg.uncheckedNodes = nil
	dawg.minimizedNodes = nil
	dawg.lastWord = nil
}

func (dawg *dawg) Print() {
	var edgeEnd edgeEnd
	for _, edgeStart := range dawg.getEdges() {
		edgeEnd = dawg.edges[edgeStart]
		if dawg.isFinal(edgeEnd.node) {
			fmt.Printf("%v:%v -> %v skipped=%v (Final)\n", edgeStart.node, string(edgeStart.ch), edgeEnd.node, edgeEnd.count)
		} else {
			fmt.Printf("%v:%v -> %v skipped=%v\n", edgeStart.node, string(edgeStart.ch), edgeEnd.node, edgeEnd.count)
		}
	}
}

func (dawg *dawg) FindAllPrefixesOf(input string) []FindResult {
	var results []FindResult
	skipped := 0
	node := dawg.root

	// for each character of the input
	for pos, letter := range input {
		// if the node is final, add a result
		if dawg.isFinal(node) {
			//log.Printf("node %v is final", node)
			results = append(results, FindResult{
				Word:  input[:pos],
				Index: skipped,
			})
		}

		// check if there is an outgoing edge for the letter
		edgeEnd, ok := dawg.edges[edgeStart{node: node, ch: letter}]
		//log.Printf("Follow %v:%v=>%v (ok=%v)", node, string(letter), edgeEnd.node, ok)
		if !ok {
			return results
		}

		// we found an edge.
		node = edgeEnd.node
		skipped += edgeEnd.count
	}

	if dawg.isFinal(node) {
		//log.Printf("node %v is final", node)
		results = append(results, FindResult{
			Word:  input,
			Index: skipped,
		})
	}

	return results
}

func (dawg *dawg) IndexOf(input string) int {
	skipped := 0
	node := dawg.root

	// for each character of the input
	for _, letter := range input {
		// check if there is an outgoing edge for the letter
		edgeEnd, ok := dawg.edges[edgeStart{node: node, ch: letter}]
		//log.Printf("Follow %v:%v=>%v (ok=%v)", node, string(letter), edgeEnd.node, ok)
		if !ok {
			// not found
			return -1
		}

		// we found an edge.
		node = edgeEnd.node
		skipped += edgeEnd.count
	}

	if dawg.isFinal(node) {
		return skipped
	}
	return -1
}

func (dawg *dawg) NumAdded() int {
	return dawg.numAdded
}

func (dawg *dawg) NumNodes() int {
	return dawg.numNodes
}

func (dawg *dawg) NumEdges() int {
	return len(dawg.edges)
}

func (dawg *dawg) checkFinished() {
	if !dawg.finished {
		panic(errors.New("DAWG was not Finished()"))
	}
}

func (dawg *dawg) minimize(downTo int) {
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

func (dawg *dawg) newNode() int {
	dawg.nextID++
	return dawg.nextID - 1
}

func (dawg *dawg) nameOf(node int) string {
	// node name is id_ch:id... for each child
	buff := bytes.Buffer{}
	for _, edge := range dawg.names[node] {
		buff.WriteString("_")
		buff.WriteString(string(edge.ch))
		buff.WriteString(":")
		buff.WriteString(strconv.Itoa(edge.node))
	}

	return buff.String()
}

func (dawg *dawg) setFinal(node int) {
	dawg.addChild(node, 0, 0)
}

func (dawg *dawg) isFinal(node int) bool {
	_, ok := dawg.edges[edgeStart{node: node, ch: 0}]
	return ok
}

func (dawg *dawg) addChild(parent int, ch rune, child int) {
	//log.Printf("Addchild %v(%v)->%v", parent, string(ch), child)
	dawg.names[parent] = append(dawg.names[parent], edgeStart{ch, child})
	dawg.edges[edgeStart{ch, parent}] = edgeEnd{node: child}
}

func (dawg *dawg) getChild(parent int, ch rune) edgeEnd {
	return dawg.edges[edgeStart{ch, parent}]
}

func (dawg *dawg) getEdges() []edgeStart {
	var edges []edgeStart
	for edge := range dawg.edges {
		edges = append(edges, edge)
	}

	sort.Slice(edges, func(a, b int) bool {
		return edges[a].node < edges[b].node
	})

	return edges
}

func (dawg *dawg) replaceChild(parent int, ch rune, child int) {
	start := edgeStart{ch, parent}
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

func (dawg *dawg) calculateSkipped(cache map[int]int, node int) int {
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

	for i, eStart := range edges {
		// if it marks the final node, then add one
		if eStart.ch == 0 {
			numReachable++
			if i != 0 {
				//log.Printf("%s", dawg.nameOf(node))
				log.Panic(errors.New("final marker must be first edge"))
			}
		} else {
			dawg.setCount(node, eStart.ch, numReachable)
			numReachable += dawg.calculateSkipped(cache, eStart.node)
		}
	}

	cache[node] = numReachable
	return numReachable
}

func (dawg *dawg) setCount(node int, ch rune, count int) {
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
