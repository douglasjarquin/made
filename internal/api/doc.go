// Package api implements made's unix-socket JSON envelope: a
// method/params/id request paired with a result/error/id response, each
// carrying an exact-match made.protocol version. It is idiomatically
// similar to herdr's own socket API but independently designed, with its
// own Go types, and is not wire-compatible with herdr.
package api
