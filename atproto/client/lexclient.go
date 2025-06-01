package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Implements the `LexClient` interface, for use with code-generated API helpers.
func (c *APIClient) LexDo(ctx context.Context, kind string, inpenc string, method string, params map[string]any, bodyobj any, out any) error {
	// some of the code here is copied from indigo:xrpc/xrpc.go

	nsid, err := syntax.ParseNSID(method)
	if err != nil {
		return err
	}

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

	req := NewAPIRequest(kind, nsid, body)

	if bodyobj != nil && inpenc != "" {
		req.Headers.Set("Content-Type", inpenc)
	}

	if params != nil {
		qp, err := ParseParams(params)
		if err != nil {
			return err
		}
		req.QueryParams = qp
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		var eb ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return &APIError{StatusCode: resp.StatusCode}
		}
		return eb.APIError(resp.StatusCode)
	}

	if out == nil {
		return nil
	}

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
			return fmt.Errorf("failed decoding JSON response body: %w", err)
		}
	}

	return nil
}
