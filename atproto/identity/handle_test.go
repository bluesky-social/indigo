package identity

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type repeatByteReader struct {
	b         byte
	remaining int64
}

func (r *repeatByteReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = r.b
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestParseTXTResp(t *testing.T) {
	assert := assert.New(t)

	did, err := parseTXTResp([]string{"did=did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	// records without the "did=" prefix are ignored
	did, err = parseTXTResp([]string{"v=spf1 -all", "did=did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	// duplicate records with the same DID are not ambiguous
	did, err = parseTXTResp([]string{"did=did:plc:ewvi7nxzyoun6zhxrhs64oiz", "did=did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	_, err = parseTXTResp([]string{})
	assert.ErrorIs(err, ErrHandleNotFound)

	_, err = parseTXTResp([]string{"v=spf1 -all"})
	assert.ErrorIs(err, ErrHandleNotFound)

	_, err = parseTXTResp([]string{"did=not-a-did"})
	assert.ErrorIs(err, ErrHandleResolutionFailed)

	// multiple records with different DIDs are ambiguous, and resolution fails
	_, err = parseTXTResp([]string{"did=did:plc:ewvi7nxzyoun6zhxrhs64oiz", "did=did:web:example.com"})
	assert.ErrorIs(err, ErrHandleResolutionFailed)
}

func TestReadAllLimited(t *testing.T) {
	assert := assert.New(t)

	got, err := readAllLimited(strings.NewReader("exact"), 5)
	assert.NoError(err)
	assert.Equal("exact", string(got))

	got, err = readAllLimited(strings.NewReader("under"), 10)
	assert.NoError(err)
	assert.Equal("under", string(got))

	_, err = readAllLimited(strings.NewReader("overflow"), 5)
	assert.ErrorIs(err, errBodyTooLarge)
}

func TestResolveHandleWellKnownBodyLimit(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	handle := syntax.Handle("example.com")

	// Exact limit with chunked/unknown Content-Length succeeds when DID is valid.
	didPrefix := "did:plc:ewvi7nxzyoun6zhxrhs64oiz"
	exactBody := didPrefix + strings.Repeat(" ", int(maxHandleBodySize)-len(didPrefix))
	dirExact := BaseDirectory{
		HTTPClient: http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1,
					Body:          io.NopCloser(strings.NewReader(exactBody)),
				}, nil
			}),
		},
	}
	did, err := dirExact.ResolveHandleWellKnown(ctx, handle)
	assert.NoError(err)
	assert.Equal(syntax.DID(didPrefix), did)

	// Exceeding limit without Content-Length errors instead of truncating to a valid DID prefix.
	overBody := didPrefix + strings.Repeat(" ", int(maxHandleBodySize)-len(didPrefix)+1)
	dirOverUnknownLen := BaseDirectory{
		HTTPClient: http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1,
					Body:          io.NopCloser(strings.NewReader(overBody)),
				}, nil
			}),
		},
	}
	_, err = dirOverUnknownLen.ResolveHandleWellKnown(ctx, handle)
	assert.ErrorIs(err, ErrHandleResolutionFailed)
	assert.Contains(err.Error(), "too large")

	// Understated Content-Length header also fails when actual body exceeds maxHandleBodySize.
	dirUnderstated := BaseDirectory{
		HTTPClient: http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: int64(len(didPrefix)),
					Body:          io.NopCloser(strings.NewReader(overBody)),
				}, nil
			}),
		},
	}
	_, err = dirUnderstated.ResolveHandleWellKnown(ctx, handle)
	assert.ErrorIs(err, ErrHandleResolutionFailed)
	assert.Contains(err.Error(), "too large")
}

func TestResolveDIDWebBodyLimit(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	did := syntax.DID("did:web:example.com")

	// Valid JSON padded to exact maxDIDBodySize succeeds with unknown Content-Length.
	docJSON := `{"id":"did:web:example.com"}`
	exactReader := io.MultiReader(
		strings.NewReader(docJSON),
		&repeatByteReader{b: ' ', remaining: maxDIDBodySize - int64(len(docJSON))},
	)
	dirExact := BaseDirectory{
		HTTPClient: http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1,
					Body:          io.NopCloser(exactReader),
				}, nil
			}),
		},
	}
	doc, err := dirExact.ResolveDID(ctx, did)
	assert.NoError(err)
	assert.Equal(did, doc.DID)

	// One byte over maxDIDBodySize with unknown Content-Length fails with ErrDIDResolutionFailed.
	overReader := io.MultiReader(
		strings.NewReader(docJSON),
		&repeatByteReader{b: ' ', remaining: maxDIDBodySize - int64(len(docJSON)) + 1},
	)
	dirOver := BaseDirectory{
		HTTPClient: http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1,
					Body:          io.NopCloser(overReader),
				}, nil
			}),
		},
	}
	_, err = dirOver.ResolveDID(ctx, did)
	assert.ErrorIs(err, ErrDIDResolutionFailed)
	assert.Contains(err.Error(), "too large")
}
