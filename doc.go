/*
Package dawg is an implemention of a Directed Acyclic Word Graph, as described
on my blog at http://stevehanov.ca/blog/?id=115

It is designed to be as memory efficient as possible. Instead of storing
a map in each node, which can consume hundreds of bytes, one map is
used to store the edges of all nodes.

When you are finished adding words, all unneeded information is
discarded.
*/
package dawg
