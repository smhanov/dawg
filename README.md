# dawg [![GoDoc](https://godoc.org/github.com/smhanov/dawg?status.svg)](https://godoc.org/github.com/smhanov/dawg)
[![CircleCI](https://circleci.com/gh/smhanov/dawg.svg?style=svg)](https://circleci.com/gh/smhanov/dawg)
Package dawg is an implemention of a Directed Acyclic Word Graph, as described on my blog at http://stevehanov.ca/blog/?id=115 It is designed to be as memory efficient as possible.

Download:
```shell
go get github.com/smhanov/dawg
```

* * *
Package dawg is an implemention of a Directed Acyclic Word Graph, as described
on my blog at http://stevehanov.ca/blog/?id=115

A DAWG provides fast lookup of all possible prefixes of words in a dictionary, as well
as the ability to get the index number of any word.

This particular implementation may be different from others because it is very memory
efficient, and it also works fast with large character sets. It can deal with
thousands of branches out of a single node without needing to go through each one.

The storage format is as small as possible. Bits are used instead of bytes so that
no space is wasted as padding, and there are no practical limitations to the number of
nodes or characters. A summary of the data format is found at the top of disk.go.

In general, to use it you first create a builder using dawg.New(). You can then
add words to the Dawg. The two restrictions are that you cannot repeat a word, and
they must be in strictly increasing alphabetical order.

After all the words are added, call Finish() which returns a dawg.Finder interface.
You can perform queries with this interface, such as finding all prefixes of a given string
which are also words, or looking up a word's index that you have previously added.

After you have called Finish() on a Builder, you may choose to write it to disk using the
Save() function. The DAWG can then be opened again later using the Load() function.
When opened from disk, no memory is used. The structure is accessed in-place on disk.

## Benchmarks

There are some benchmarks in this project:

https://github.com/timurgarif/go-fsa-trie-bench

The library is optimized to take less memory or no-memory if accessing a file. We easily beat all 
the alternatives in this area, using only 520KB compared to others which take from 3.6MB to 32MB 
to store the same dictionary. However, the tradeoff is that  the bit-level accesses cause it to 
take 10X as along to lookup words. 
