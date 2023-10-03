package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type AuthInfo struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	AdminToken string `json:"adminToken"`
	Handle     string `json:"handle"`
	Did        string `json:"did"`
}

type Client struct {
	Client    *http.Client
	Auth      *AuthInfo
	Host      string
	UserAgent *string
	Headers   map[string]string
}

// Context can include additional headers, including "cache busting", metrics collection, etc.

func (c *Client) Login(ctx context.Context, ident syntax.AtIdentifier, password string) (*AuthInfo, error) {
	return nil, fmt.Errorf("XXX: not implemented")
}

func (c *Client) Get(ctx context.Context, endpoint string, params map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("XXX: not implemented")
}

func (c *Client) GetBytes(ctx context.Context, endpoint string, params map[string]interface{}) ([]byte, error) {
	return nil, fmt.Errorf("XXX: not implemented")
}

func (c *Client) Post(ctx context.Context, endpoint string, body, params map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("XXX: not implemented")
}

// TODO: return stdlib HTTP wrapper?
func (c *Client) PostBytes(ctx context.Context, endpoint string, contentType string, body, params map[string]interface{}) ([]byte, error) {
	return nil, fmt.Errorf("XXX: not implemented")
}

type StreamEvent struct {
	// header meta
	// body bytes
}

func (c *Client) Subscribe(ctx context.Context, endpoint string, ch <-chan StreamEvent, params map[string]interface{}) error {
	return fmt.Errorf("XXX: not implemented")
}
