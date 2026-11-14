// Package httpfake builds fake HTTP servers for tests. A fake server
// records each request that it receives and then answers with a JSON
// body. The record of a request holds its method, its path, its raw
// query, its Authorization header, and its JSON body decoded into a map.
// A test drives a client against the fake and then reads the records to
// check which requests the client sent.
//
// A fake with routes records each request in its handler and picks the
// answer from the record. A test that needs one answer for every request
// starts a server that serves that answer. The server also checks that
// each request carries the User-Agent header of the client under test.
//
// The fakes of the service of Agg's Destiny Vault Manager and of the
// Bungie platform API build on this package, so it imports neither API.
package httpfake
