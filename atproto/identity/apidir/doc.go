/*
Identity Directory implementation which makes HTTP requests to a dedicated identity service.

Implements both `identity.Directory` (Lookup methods) and `identity.Resolver` (Resolve methods). You may want to wrap this with a small in-process cache, eg `identity.CacheDirectory`.

Makes use of standard Lexicons:

- com.atproto.identity.resolveHandle
- com.atproto.identity.resolveDid
- com.atproto.identity.resolveIdentity
*/
package apidir
