package xrpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/util"
	"github.com/carlmjohnson/versioninfo"
)

type Client struct {
	// Client is an HTTP client to use. If not set, defaults to http.RobustHTTPClient().
	Client     *http.Client
	Auth       *AuthInfo
	AdminToken *string
	Host       string
	UserAgent  *string
	Headers    map[string]string
}

func (c *Client) getClient() *http.Client {
	if c.Client == nil {
		return util.RobustHTTPClient()
	}
	return c.Client
}

type XRPCRequestType int

type AuthInfo struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	Handle     string `json:"handle"`
	Did        string `json:"did"`
}

type XRPCError struct {
	ErrStr  string `json:"error"`
	Message string `json:"message"`
}

func (xe *XRPCError) Error() string {
	return fmt.Sprintf("%s: %s", xe.ErrStr, xe.Message)
}

type Error struct {
	StatusCode int
	Wrapped    error
	Ratelimit  *RatelimitInfo
}

func (e *Error) Error() string {
	// Preserving "XRPC ERROR %d" prefix for compatibility - previously matching this string was the only way
	// to obtain the status code.
	if e.Wrapped == nil {
		return fmt.Sprintf("XRPC ERROR %d", e.StatusCode)
	}
	if e.StatusCode == http.StatusTooManyRequests && e.Ratelimit != nil {
		return fmt.Sprintf("XRPC ERROR %d: %s (throttled until %s)", e.StatusCode, e.Wrapped, e.Ratelimit.Reset.Local())
	}
	return fmt.Sprintf("XRPC ERROR %d: %s", e.StatusCode, e.Wrapped)
}

func (e *Error) Unwrap() error {
	if e.Wrapped == nil {
		return nil
	}
	return e.Wrapped
}

func (e *Error) IsThrottled() bool {
	return e.StatusCode == http.StatusTooManyRequests
}

func errorFromHTTPResponse(resp *http.Response, err error) error {
	r := &Error{
		StatusCode: resp.StatusCode,
		Wrapped:    err,
	}
	if resp.Header.Get("ratelimit-limit") != "" {
		r.Ratelimit = &RatelimitInfo{
			Policy: resp.Header.Get("ratelimit-policy"),
		}
		if n, err := strconv.ParseInt(resp.Header.Get("ratelimit-reset"), 10, 64); err == nil {
			r.Ratelimit.Reset = time.Unix(n, 0)
		}
		if n, err := strconv.ParseInt(resp.Header.Get("ratelimit-limit"), 10, 64); err == nil {
			r.Ratelimit.Limit = int(n)
		}
		if n, err := strconv.ParseInt(resp.Header.Get("ratelimit-remaining"), 10, 64); err == nil {
			r.Ratelimit.Remaining = int(n)
		}
	}
	return r
}

type RatelimitInfo struct {
	Limit     int
	Remaining int
	Policy    string
	Reset     time.Time
}

const (
	Query = XRPCRequestType(iota)
	Procedure
)

// makeParams converts a map of string keys and any values into a URL-encoded string.
// If a value is a slice of strings, it will be joined with commas.
// Generally the values will be strings, numbers, booleans, or slices of strings
func makeParams(p map[string]any) string {
	params := url.Values{}
	for k, v := range p {
		if s, ok := v.([]string); ok {
			for _, v := range s {
				params.Add(k, v)
			}
		} else {
			params.Add(k, fmt.Sprint(v))
		}
	}

	return params.Encode()
}

func (c *Client) Do(ctx context.Context, kind XRPCRequestType, inpenc string, method string, params map[string]interface{}, bodyobj interface{}, out interface{}) error {
	var body io.Reader
	if bodyobj != nil {
		if rr, ok := bodyobj.(io.Reader); ok {
			body = rr
		} else {
			b, err := json.Marshal(bodyobj)
			if err != nil {
				return err
			}

			body = bytes.NewReader(b)
		}
	}

	var m string
	switch kind {
	case Query:
		m = "GET"
	case Procedure:
		m = "POST"
	default:
		return fmt.Errorf("unsupported request kind: %d", kind)
	}

	var paramStr string
	if len(params) > 0 {
		paramStr = "?" + makeParams(params)
	}

	req, err := http.NewRequest(m, c.Host+"/xrpc/"+method+paramStr, body)
	if err != nil {
		return err
	}

	if bodyobj != nil && inpenc != "" {
		req.Header.Set("Content-Type", inpenc)
	}
	if c.UserAgent != nil {
		req.Header.Set("User-Agent", *c.UserAgent)
	} else {
		req.Header.Set("User-Agent", "indigo/"+versioninfo.Short())
	}

	if c.Headers != nil {
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
	}

	// use admin auth if we have it configured and are doing a request that requires it
	if c.AdminToken != nil && (strings.HasPrefix(method, "com.atproto.admin.") || strings.HasPrefix(method, "tools.ozone.") || method == "com.atproto.account.createInviteCode" || method == "com.atproto.server.createInviteCodes") {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+*c.AdminToken)))
	} else if c.Auth != nil {
		req.Header.Set("Authorization", "Bearer "+c.Auth.AccessJwt)
	}

	resp, err := c.getClient().Do(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var xe XRPCError
		if err := json.NewDecoder(resp.Body).Decode(&xe); err != nil {
			return errorFromHTTPResponse(resp, fmt.Errorf("failed to decode xrpc error message: %w", err))
		}
		return errorFromHTTPResponse(resp, &xe)
	}

	if out != nil {
		if buf, ok := out.(*bytes.Buffer); ok {
			if resp.ContentLength < 0 {
				_, err := io.Copy(buf, resp.Body)
				if err != nil {
					return fmt.Errorf("reading response body: %w", err)
				}
			} else {
				n, err := io.CopyN(buf, resp.Body, resp.ContentLength)
				if err != nil {
					return fmt.Errorf("reading length delimited response body (%d < %d): %w", n, resp.ContentLength, err)
				}
			}
		} else {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decoding xrpc response: %w", err)
			}
		}
	}

	return nil
}
