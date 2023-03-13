package labeling

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

type MicroNSFWImgLabeler struct {
	Client   http.Client
	Endpoint string
}

type MicroNSFWImgResp struct {
	Drawings float64 `json:"drawings"`
	Hentai   float64 `json:"hentai"`
	Neutral  float64 `json:"neutral"`
	Porn     float64 `json:"porn"`
	Sexy     float64 `json:"sexy"`
}

func NewMicroNSFWImgLabeler(url string) MicroNSFWImgLabeler {
	client := http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	return MicroNSFWImgLabeler{
		Client:   client,
		Endpoint: url,
	}
}

func (mnil *MicroNSFWImgLabeler) summarizeResp(resp MicroNSFWImgResp) []string {
	var labels []string

	// TODO(bnewbold): these score cutoffs are kind of arbitrary
	if resp.Porn > 0.90 {
		labels = append(labels, "porn")
	}
	if resp.Hentai > 0.90 {
		labels = append(labels, "hentai")
	}
	if resp.Sexy > 0.90 {
		labels = append(labels, "sexy")
	}
	return labels
}

func (mnil *MicroNSFWImgLabeler) LabelBlob(blob lexutil.Blob, blobBytes []byte) ([]string, error) {

	log.Infof("sending blob to micro-NSFW-img cid=%s mimetype=%s size=%d", blob.Cid, blob.MimeType, len(blobBytes))

	// generic HTTP form file upload, then parse the response JSON
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", blob.Cid)
	if err != nil {
		return nil, err
	}

	_, err = part.Write(blobBytes)
	if err != nil {
		return nil, err
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", mnil.Endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "labelmaker/0.0")

	res, err := mnil.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("micro-NSFW-img request failed: %v", err)
	}
	defer res.Body.Close()

	resp, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read micro-NSFW-img resp body: %v", err)
	}

	var nsfwScore MicroNSFWImgResp
	if err = json.Unmarshal(resp, &nsfwScore); err != nil {
		return nil, fmt.Errorf("failed to parse micro-NSFW-img resp JSON: %v", err)
	}
	scoreJson, _ := json.Marshal(nsfwScore)
	log.Infof("micro-NSFW-img result cid=%s scores=%v", blob.Cid, string(scoreJson))
	return mnil.summarizeResp(nsfwScore), nil
}
