package dawg_test

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/smhanov/dawg"
)

func testsWords() []string {
	return []string{
		"hello",
		"jellow",
	}
}

func createDawg(words []string) (dawg.Builder, dawg.Finder) {
	dawg := dawg.New()
	for _, word := range words {
		dawg.Add(word)
	}

	return dawg, dawg.Finish()
}

func testDawg(t *testing.T, dawg dawg.Finder, words []string) {
	added := dawg.NumAdded()
	if added != len(words) {
		t.Errorf("NumWords() returned %d, expected %d", added, len(words))
	}

	for i, word := range words {
		index := dawg.IndexOf(word)

		if index != i {
			t.Errorf("Index returned should be %v, not %v", i, index)
		}
	}
}

func runTest(t *testing.T, words []string) dawg.Finder {
	builder, finder := createDawg(words)
	testDawg(t, finder, words)

	// Now try the disk version
	_, err := builder.Save("test.dawg")
	if err != nil {
		log.Panic(err)
	}

	saved, err := dawg.Load("test.dawg")
	if err != nil {
		log.Panic(err)
	}

	saved.Print()

	testDawg(t, saved, words)

	return finder
}

func TestZeroLengthWord(t *testing.T) {
	runTest(t, []string{
		"",
	})
}

func TestSingleEntry(t *testing.T) {
	runTest(t, []string{
		"a",
	})
}

func TestHelloJello(t *testing.T) {
	runTest(t, []string{
		"hello",
		"jello",
	})
}

func testPrefixes(t *testing.T, words []string, word string, shouldbe []dawg.FindResult) {
	_, finder := createDawg(words)

	results := finder.FindAllPrefixesOf(word)

	if len(results) != len(shouldbe) {
		t.Errorf("Got %v but should be %v", results, shouldbe)
	}

	for i, result := range results {
		if result != shouldbe[i] {
			t.Errorf("Got %v but should be %v", results, shouldbe)
			break
		}
	}
}

func TestPrefixes(t *testing.T) {
	words := []string{
		"",
		"blip",
		"cat",
		"catnip",
		"cats",
	}

	testPrefixes(t, words, "catsup", []dawg.FindResult{
		{Word: "", Index: 0},
		{Word: "cat", Index: 2},
		{Word: "cats", Index: 4},
	})
}

func readDictWords(t *testing.T) []string {
	dict := "/usr/share/dict/words"
	if _, err := os.Stat(dict); os.IsNotExist(err) {
		t.Logf("Skipping full dictionary test; can't find %s", dict)
		return nil
	}

	file, err := os.Open(dict)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	var words []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		words = append(words, scanner.Text())
	}
	return words
}

func TestFullDict(t *testing.T) {
	words := readDictWords(t)
	dawg := runTest(t, words)
	t.Logf("DAWG has %v words, %v nodes, %v edges",
		dawg.NumAdded(), dawg.NumNodes(), dawg.NumEdges())
}

func ExampleNew() {
	dawg := dawg.New()

	dawg.Add("blip")   // index 0
	dawg.Add("cat")    // index 1
	dawg.Add("catnip") // index 2
	dawg.Add("cats")   // index 3

	finder := dawg.Finish()

	for _, result := range finder.FindAllPrefixesOf("catsup") {
		fmt.Printf("Found prefix %s, index %d\n", result.Word, result.Index)
	}

	// Output:
	// Found prefix cat, index 1
	// Found prefix cats, index 3
}
