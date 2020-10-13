package dawg

import (
	"fmt"
	"sort"
)

// StringHash implements the FNV32A hash for strings,
// taking d as a parameter to provide a variation of the hash
func StringHash(d int32, str string) int {
	result := int(d)
	if d == 0 {
		result = 0x01000193
	}

	// Use the FNV algorithm from http://isthe.com/chongo/tech/comp/fnv/
	for _, c := range []byte(str) {
		result = ((result * 0x01000193) ^ int(c)) & 0xffffffff
	}

	return result
}

// CreateMinimalPerfectHash creates a minimal perfect hash for an array
// of items. Size is the number of items, and hash is a hash function.
// The hash function takes d, a variant, and i, the index of the item to hash,
// and returns its hashed value.
//
// The result is two arrays. The first, G, contains the d value to use
// for the secondary hash function. The second contains a shuffling of the
// items, indicating which item should be placed in each slot.
func CreateMinimalPerfectHash(size int, hash func(d int32, i int) int) ([]int32, []int) {
	// Step 1: Place all of the keys into buckets
	buckets := make([][]int, size, size)
	G := make([]int32, size, size)
	values := make([]int, size, size)

	for i := range values {
		values[i] = -1
	}

	for item := 0; item < size; item++ {
		b := int(hash(0, item)) % size
		buckets[b] = append(buckets[b], item)
	}

	// Step 2: Sort the buckets and process the ones with the most items first.
	sort.Slice(buckets, func(i, j int) bool {
		return len(buckets[i]) >= len(buckets[j])
	})

	var maxD int32
	var b int
	for b = 0; b < len(buckets); b++ {
		bucket := buckets[b]
		//log.Printf("Placing bucket %v", bucket)
		if len(bucket) <= 1 {
			break
		}

		d := int32(1)
		item := 0
		slots := make([]int, 0, len(bucket))

		// Repeatedly try different values of d until we find a hash function
		// that places all items in the bucket into free slots
		for item < len(bucket) {
			slot := int(hash(d, bucket[item])) % size
			found := false
			for _, pos := range slots {
				if slot == pos {
					found = true
					break
				}
			}
			if values[slot] != -1 || found {
				//log.Printf("   Add %v to slot %v", items.Index(bucket[item]), slot)
				d++
				item = 0
				slots = slots[:0]
				//log.Printf("   Abort! Retry bucket %v with d=%v", b, d)
			} else {
				//log.Printf("   Add %v to slot %v", items.Index(bucket[item]), slot)
				item++
				slots = append(slots, slot)
			}
		}

		if d > maxD {
			maxD = d
		}

		G[int(hash(0, bucket[0]))%size] = int32(d)
		for i := 0; i < len(bucket); i++ {
			//log.Printf("  (move %v to slot %v)", items.Index(bucket[i]), slots[i])
			values[slots[i]] = bucket[i]
		}

		if (b % 1000) == 0 {
			fmt.Printf("bucket %d    \r", b)
		}
	}

	// Only buckets with 1 item remain. Process them more quickly by directly
	// placing them into a free slot. Use a negative value of d to indicate
	// this.
	freelist := make([]int, 0, size)
	for i := 0; i < size; i++ {
		if values[i] == -1 {
			freelist = append(freelist, i)
		}
	}

	for nb := b; nb < len(buckets); nb++ {
		bucket := buckets[nb]
		if len(bucket) == 0 || len(freelist) == 0 {
			break
		}
		slot := freelist[len(freelist)-1]
		freelist = freelist[:len(freelist)-1]
		// We subtract one to ensure it's negative even if the zeroeth slot was
		// used.
		G[int(hash(0, bucket[0]))%size] = int32(-slot - 1)
		//log.Printf(" Place %v in slot %v", items.Index(bucket[0]), slot)
		values[slot] = bucket[0]
		if (b % 1000) == 0 {
			fmt.Printf("bucket %d    \r", b)
		}
	}

	//log.Printf("maxD was %d", maxD)

	return G, values
}
