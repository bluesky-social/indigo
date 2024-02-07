package visual

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"

	"github.com/carlmjohnson/versioninfo"
)

type AbyssClient struct {
	Client          http.Client
	Host            string
	Password        string
	RatelimitBypass string
}

func NewAbyssClient(host, password, ratelimitBypass string) AbyssClient {
	return AbyssClient{
		Client:          *util.RobustHTTPClient(),
		Host:            host,
		Password:        password,
		RatelimitBypass: password,
	}
}

func (ac *AbyssClient) ScanBlob(ctx context.Context, blob lexutil.LexBlob, blobBytes []byte, params map[string]string) (*AbyssScanResp, error) {

	slog.Debug("sending blob to abyss", "cid", blob.Ref.String(), "mimetype", blob.MimeType, "size", len(blobBytes))

	body := bytes.NewBuffer(blobBytes)
	req, err := http.NewRequest("POST", ac.Host+"/xrpc/com.atproto.unspecced.scanBlob", body)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()

	req.SetBasicAuth("admin", ac.Password)
	req.Header.Add("Content-Type", blob.MimeType)
	req.Header.Add("Content-Length", fmt.Sprintf("%d", blob.Size))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "indigo-automod/"+versioninfo.Short())
	if ac.RatelimitBypass != "" {
		req.Header.Set("x-ratelimit-bypass", ac.RatelimitBypass)
	}

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		abyssAPIDuration.Observe(duration.Seconds())
	}()

	req = req.WithContext(ctx)
	res, err := ac.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("abyss request failed: %v", err)
	}
	defer res.Body.Close()

	abyssAPICount.WithLabelValues(fmt.Sprint(res.StatusCode)).Inc()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("abyss request failed statusCode=%d", res.StatusCode)
	}

	respBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read abyss resp body: %v", err)
	}

	var respObj AbyssScanResp
	if err := json.Unmarshal(respBytes, &respObj); err != nil {
		return nil, fmt.Errorf("failed to parse abyss resp JSON: %v", err)
	}
	slog.Info("abyss-scan-response", "cid", blob.Ref.String(), "obj", respObj)
	return &respObj, nil
}
