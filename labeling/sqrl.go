package labeling

import (
	//"encoding/json"
	"net/http"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

type SQRLLabeler struct {
	Client   http.Client
	Endpoint string
}

func NewSQRLLabeler(url string) SQRLLabeler {
	client := http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	return SQRLLabeler{
		Client:   client,
		Endpoint: url,
	}
}

func (mnil *MicroNSFWImgLabeler) LabelPost(post appbsky.FeedPost) ([]string, error) {
	var labels []string
	return labels, nil
}

func (mnil *MicroNSFWImgLabeler) LabelProfile(post appbsky.FeedPost) ([]string, error) {
	var labels []string
	return labels, nil
}
