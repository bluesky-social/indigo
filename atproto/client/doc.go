/*
General-purpose client for atproto "XRPC" HTTP API endpoints.

[APIClient] wraps an [http.Client] and provides an ergonomic atproto-specific (but not Lexicon-specific) interface for "Query" (GET) and "Procedure" (POST) endpoints. It does not support "Event Stream" (WebSocket) endpoints. The client is expected to be used with a single host at a time, though it does have special support ([APIClient.WithService]) for proxied service requests when connected to a PDS host. The client does not authenticate requests by default, but supports pluggable authentication methods (see below). The [APIReponse] struct represents a generic API request, and helps with conversion to an [http.Request].

The [APIError] struct can represent a generic API error response (eg, an HTTP response with a 4xx or 5xx response code), including the 'error' and 'message' JSON response fields expected with atproto. It is intended to be used with [errors.Is] in error handling, or to provide helpful error messages.

The [AuthMethod] interface allows [APIClient] to work with multiple forms of authentication in atproto. It is expected that more complex auth systems (eg, those using signed JWTs) will be implemented in separate packages, but this package does include two simple auth methods:

- [PasswordAuth] is the original PDS user auth method, using access and refresh tokens.
- [AdminAuth] is simple HTTP Basic authentication for administrative requests, as implemented by many atproto services (Relay, Ozone, PDS, etc).

## Design Notes

Several [AuthMethod] implementations are expected to require retrying entire request at unexpected times. For example, unexpected OAuth DPoP nonce changes, or unexpected password session token refreshes. The auth method may also need to make requests to other servers as part of the refresh process (eg, OAuth when working with a PDS/entryway split). This means that requests should be "retryable" as often as possible. This is mostly a concern for Procedures (HTTP POST) with a non-empty body. The [http.Client] will attempt to "unclose" some common [io.ReadCloser] types (like [bytes.Buffer]), but others may need special handling, using the [APIRequest.GetBody] method. This package will try to make types implementing [io.Seeker] tryable; this helps with things like passing in a open file descriptor for file uploads.

In theory, the [http.RoundTripper] interface could have been used instead of [AuthMethod]; or auth methods could have been injected in to [http.Client] instances directly. This package avoids this pattern for a few reasons. The first is that wrangling layered stacks of [http.RoundTripper] can become cumbersome. Calling code may want to use [http.Client] variants which add observability, retries, circuit-breaking, or other non-auth customization. Secondly, some atproto auth methods will require requests to other servers or endpoints, and having a common [http.Client] to re-use for these requests makes sense. Finally, several atproto auth methods need to know the target endpoint as an NSID; while this could be re-parsed from the request URL, it is simpler and more reliable to pass it as an argument.

This package tries to use minimal dependencies beyond the Go standard library, to make it easy to reference as a dependency. It does require the [github.com/bluesky-social/indigo/atproto/syntax] and [github.com/bluesky-social/indigo/atproto/identity] sibling packages. In particular, this package does not include any auth methods requiring JWTs, to avoid adding any specific JWT implementation as a dependency.
*/
package client
