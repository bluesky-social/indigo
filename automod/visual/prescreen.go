package visual

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"sync"
	"time"
)

const failureThresh = 10

type PreScreenClient struct {
	Host  string
	Token string

	breakerEOL time.Time
	breakerLk  sync.Mutex
	failures   int

	c *http.Client
}

func NewPreScreenClient(host, token string) *PreScreenClient {
	c := &http.Client{
		Timeout: time.Second * 5,
	}

	return &PreScreenClient{
		Host:  host,
		Token: token,
		c:     c,
	}
}

func (c *PreScreenClient) available() bool {
	c.breakerLk.Lock()
	defer c.breakerLk.Unlock()
	if c.breakerEOL.IsZero() {
		return true
	}

	if time.Now().After(c.breakerEOL) {
		c.breakerEOL = time.Time{}
		return true
	}

	return false
}

func (c *PreScreenClient) recordCallResult(success bool) {
	c.breakerLk.Lock()
	defer c.breakerLk.Unlock()
	if !c.breakerEOL.IsZero() {
		return
	}

	if success {
		c.failures = 0
	} else {
		c.failures++
		if c.failures > failureThresh {
			c.breakerEOL = time.Now().Add(time.Minute)
			c.failures = 0
		}
	}
}

func (c *PreScreenClient) PreScreenImage(ctx context.Context, blob []byte) (string, error) {
	if !c.available() {
		return "", fmt.Errorf("pre-screening temporarily unavailable")
	}

	res, err := c.checkImage(ctx, blob)
	if err != nil {
		c.recordCallResult(false)
		return "", err
	}

	c.recordCallResult(true)
	return res, nil
}

type PreScreenResult struct {
	Result string `json:"result"`
}

func (c *PreScreenClient) checkImage(ctx context.Context, data []byte) (string, error) {
	url := c.Host + "/predict"

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("files", "image")
	if err != nil {
		return "", err
	}

	part.Write(data)

	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return "", err
	}

	req = req.WithContext(ctx)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out PreScreenResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}

	return out.Result, nil
}
