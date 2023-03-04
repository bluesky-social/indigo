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
)

type Client struct {
	Client     *http.Client
	Auth       *AuthInfo
	AdminToken *string
	Host       string
	UserAgent  *string
}

func (c *Client) getClient() *http.Client {
	if c.Client == nil {
		return http.DefaultClient
	}
	return c.Client
}

type XRPCRequestType int

type AuthInfo struct {
	AccessJwt      string `json:"accessJwt"`
	RefreshJwt     string `json:"refreshJwt"`
	Handle         string `json:"handle"`
	Did            string `json:"did"`
	DeclarationCid string `json:"declarationCid"`
}

const (
	Query = XRPCRequestType(iota)
	Procedure
)

func makeParams(p map[string]interface{}) string {
	var parts []string
	for k, v := range p {
		parts = append(parts, fmt.Sprintf("%s=%s", k, url.QueryEscape(fmt.Sprint(v))))
	}

	return strings.Join(parts, "&")
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
		req.Header.Set("User-Agent", "indigo/0.0")
	}

	// use admin auth if we have it configured and are doing a request that requires it
	if c.AdminToken != nil && (strings.HasPrefix(method, "com.atproto.admin.") || method == "com.atproto.account.createInviteCode") {
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
		var i interface{}
		_ = json.NewDecoder(resp.Body).Decode(&i)
		fmt.Println("debug body response: ", i)
		return fmt.Errorf("XRPC ERROR %d: %s", resp.StatusCode, resp.Status)
	}

	if out != nil {
		if buf, ok := out.(*bytes.Buffer); ok {
			io.Copy(buf, resp.Body)
		} else {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decoding xrpc response: %w", err)
			}
		}
	}

	return nil
}
