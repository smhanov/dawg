package dawg_test

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"testing"

	"github.com/milden6/dawg"
)

func testsWords() []string {
	return []string{
		"hello",
		"jellow",
	}
}

func createDawg(words []string) dawg.Finder {
	dawg := dawg.New()
	for _, word := range words {
		dawg.Add(word)
	}

	return dawg.Finish()
}

func testDawg(t *testing.T, dawg dawg.Finder, words []string) {
	added := dawg.NumAdded()
	if added != len(words) {
		t.Errorf("NumWords() returned %d, expected %d", added, len(words))
	}

	for i, word := range words {
		index := dawg.IndexOf(word)

		if index != i {
			log.Panicf("Index returned should be %v, not %v", i, index)
		}

		wordFound, _ := dawg.AtIndex(i)
		if wordFound != word {
			log.Panicf("AtIndex(%d) should be %s, not %s", i, word, wordFound)
		}
	}
}

func runTest(t *testing.T, words []string) dawg.Finder {
	finder := createDawg(words)
	//finder.Print()
	testDawg(t, finder, words)

	// Now try the disk version
	_, err := finder.Save("test.dawg")
	if err != nil {
		log.Panic(err)
	}

	//f, err := os.Open("test.dawg")
	//dawg.DumpFile(f)
	//f.Close()

	saved, err := dawg.Load("test.dawg")
	if err != nil {
		log.Panic(err)
	}

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

func Test5Words(t *testing.T) {
	runTest(t, []string{
		"",
		"blip",
		"cat",
		"catnip",
		"cats",
	})
}

func testPrefixes(t *testing.T, words []string, word string, shouldbe []dawg.FindResult) {
	finder := createDawg(words)

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

	sort.Slice(words, func(i, j int) bool {
		return words[i] < words[j]
	})

	var unique []string
	unique = append(unique, words[0])
	for i := 1; i < len(words); i++ {
		if words[i] != words[i-1] {
			unique = append(unique, words[i])
		}
	}
	return unique
}

func TestFullDict(t *testing.T) {
	words := readDictWords(t)
	dawg := runTest(t, words)
	t.Logf("DAWG has %v words, %v nodes, %v edges",
		dawg.NumAdded(), dawg.NumNodes(), dawg.NumEdges())
}

func TestEnumerate(t *testing.T) {
	words := []string{
		"",
		"blip",
		"cat",
		"catnip",
		"cats",
		"zzz",
	}

	finder := createDawg(words)
	finder.Print()

	total := 0
	// test: when we get to catn, avoid descending
	// when we get to cats, stop altogether.
	finder.Enumerate(func(index int, word []rune, final bool) int {
		if final {
			total += 1
		}

		switch string(word) {
		case "":
			if index != 0 || !final {
				t.Errorf("Bad index at %v %v %v", index, string(word), final)
			}
		case "blip":
			if index != 1 || !final {
				t.Errorf("Bad index at %v %v %v", index, string(word), final)
			}
		case "catn":
			return dawg.Skip
		case "catni":
			fallthrough
		case "catnip":
			t.Error("Should not have got to catni")
		case "cats":
			return dawg.Stop
		case "zzz":
			t.Error("Stop had no effect.")
		}
		return dawg.Continue
	})

	if total != 4 {
		t.Errorf("Did not enumerate expected number of words; only got %d", total)
	}
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
