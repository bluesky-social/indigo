/*
Package identity provides types and routines for resolving handles and DIDs from the network

The two main abstractions are a Catalog interface for identity service implementations, and an Identity structure which represents core identity information relevant to atproto. The Catalog interface can be nested, somewhat like HTTP middleware, to provide caching, observability, or other bespoke needs in more complex systems.

Much of the implementation of this SDK is based on existing code in indigo:api/extra.go
*/
package identity
