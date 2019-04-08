package dawg

import (
	"testing"
)

func testMph(t *testing.T, words []string) {
	G, permute := minimalPerfectHash(words, func(d int32, i int) int {
		return stringHash(d, words[i])
	})

	words2 := make([]string, len(words), len(words))
	for dest, src := range permute {
		words2[dest] = words[src]
	}

	//for i, word := range words2 {
	//log.Printf("%d %s %d %d", G[i], word, int(hash(0, word))%len(G), int(hash(G[i], word))%len(G))
	//}

	for _, word := range words2 {
		d := G[int(stringHash(0, word))%len(G)]
		//log.Printf("Word %s hashes to %d, D=%d", word, int(hash(0, word))%len(G), d)
		var result string
		if d < 0 {
			result = words2[-d-1]
			//log.Printf("   Second hash is %d", -d-1)
		} else {
			result = words2[int(stringHash(d, word))%len(words2)]
			//log.Printf("   Second hash is %d", int(hash(d, word))%len(G))
		}

		if word != result {
			t.Errorf("Expected %s got %s", word, result)
		}
	}
}

func TestMph_5Words(t *testing.T) {
	testMph(t, []string{
		"apple",
		"banana",
		"hello",
		"how",
		"what",
	})
}

func TestMph_1Words(t *testing.T) {
	testMph(t, []string{
		"alpha",
	})
}

func TestMph_0Words(t *testing.T) {
	testMph(t, []string{})
}
