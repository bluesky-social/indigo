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
	"strings"
	"sync"

	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/version"
)

type Client struct {
	Client     *http.Client
	Auth       *AuthInfo
	AdminToken *string
	Host       string
	UserAgent  *string
	Headers    map[string]string
	Mux        sync.RWMutex
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
			params.Add(k, strings.Join(s, ","))
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
		req.Header.Set("User-Agent", "indigo/"+version.Version)
	}

	if c.Headers != nil {
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
	}

	// use admin auth if we have it configured and are doing a request that requires it
	if c.AdminToken != nil && (strings.HasPrefix(method, "com.atproto.admin.") || method == "com.atproto.account.createInviteCode" || method == "com.atproto.server.createInviteCodes") {
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
			return fmt.Errorf("failed to decode xrpc error message (status: %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("XRPC ERROR %d: %w", resp.StatusCode, &xe)
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
