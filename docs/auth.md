# Auth

The auth system uses two tokens, an access token and a refresh token.

The access token is a jwt with the following values:
```
scope: "com.atproto.access"
sub: <the users DID>
iat: the current time, in unix epoch seconds
exp: the expiry date, usually around an hour, but at least 15 minutes
```

The refresh token is a jwt with the following values:
```
scope: "com.atproto.refresh"
sub: <the users DID>
iat: the current time, in unix epoch seconds
exp: the expiry date, usually around a week, must be significantly longer than the access token
jti: a unique identifier for this token
```

The access token is what is used for all requests, however since it expires
quickly, it must be refreshed periodically using the refresh token.
When the refresh token is used, it must be marked as deleted, and the new token then replaces it.
Note: The old access token is not necessarily disabled at that point of refreshing.

