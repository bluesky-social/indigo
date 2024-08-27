package visual

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"time"
)

type PreScreenClient struct {
	Host  string
	Token string

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

func (c *PreScreenClient) PreScreenImage(blob []byte) (string, error) {
	return c.checkImage(blob)
}

type PreScreenResult struct {
	Result string `json:"result"`
}

func (c *PreScreenClient) checkImage(data []byte) (string, error) {
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
