package labeling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/version"
)

type SQRLLabeler struct {
	Client   http.Client
	Endpoint string
}

type SQRLRequest struct {
	Type    string                `json:"type"`
	Post    *appbsky.FeedPost     `json:"post"`
	Profile *appbsky.ActorProfile `json:"profile"`
}

type SQRLRequest_Wrap struct {
	EventData SQRLRequest `json:"EventData"`
}

type SQRLResponse struct {
	Allow    bool                         `json:"allow"`
	Verdict  SQRLResponse_Verdict         `json:"verdict"`
	Rules    map[string]SQRLResponse_Rule `json:"rules"`
	Features map[string]any               `json:"features"`
}

type SQRLResponse_Verdict struct {
	BlockRules     []string `json:"blockRules"`
	WhitelistRules []string `json:"whitelistRules"`
}

type SQRLResponse_Rule struct {
	Reason string `json:"reason"`
}

func NewSQRLLabeler(url string) SQRLLabeler {
	return SQRLLabeler{
		Client:   *util.RobustHTTPClient(),
		Endpoint: url,
	}
}

func (sl *SQRLLabeler) submitEvent(sqlrReq SQRLRequest) (*SQRLResponse, error) {

	wrapped := SQRLRequest_Wrap{EventData: sqlrReq}
	bodyJson, err := json.Marshal(wrapped)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", sl.Endpoint+"?features=EventType", bytes.NewBuffer(bodyJson))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "labelmaker/"+version.Version)

	res, err := sl.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SQRL request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("SQRL request failed  statusCode=%d", res.StatusCode)
	}

	respBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read SQRL resp body: %v", err)
	}

	var respObj SQRLResponse
	if err := json.Unmarshal(respBytes, &respObj); err != nil {
		return nil, fmt.Errorf("failed to parse SQRL resp JSON: %v", err)
	}
	respJson, _ := json.Marshal(respObj)
	log.Infof("SQRL result json=%v", string(respJson))
	return &respObj, nil
}

func (sl *SQRLLabeler) LabelPost(ctx context.Context, post appbsky.FeedPost) ([]string, error) {
	var labels []string
	req := SQRLRequest{
		Type: "post",
		Post: &post,
	}
	resp, err := sl.submitEvent(req)
	if err != nil {
		return nil, err
	}
	for name, _ := range resp.Rules {
		if name == "TooMuchCrypto" {
			labels = append(labels, "repo:crypto-shill")
		}
	}
	return labels, nil
}

func (sl *SQRLLabeler) LabelProfile(ctx context.Context, profile appbsky.ActorProfile) ([]string, error) {
	var labels []string
	req := SQRLRequest{
		Type:    "profile",
		Profile: &profile,
	}
	resp, err := sl.submitEvent(req)
	if err != nil {
		return nil, err
	}
	for name, _ := range resp.Rules {
		if name == "TooMuchCrypto" {
			labels = append(labels, "repo:crypto-shill")
		}
	}
	return labels, nil
}
