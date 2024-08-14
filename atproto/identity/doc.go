/*
Package identity provides types and routines for resolving handles and DIDs from the network

The two main abstractions are a Directory interface for identity service implementations, and an Identity struct which represents core identity information relevant to atproto. The Directory interface can be nested, somewhat like HTTP middleware, to provide caching, observability, or other bespoke needs in more complex systems.
*/
package identity
