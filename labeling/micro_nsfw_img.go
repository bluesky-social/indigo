package labeling

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

type MicroNswfImgResp struct {
	Drawings float64 `json:"drawings"`
	Hentai   float64 `json:"hentai"`
	Neutral  float64 `json:"neutral"`
	Porn     float64 `json:"porn"`
	Sexy     float64 `json:"sexy"`
}

func PostMicroNsfwImg(url string, blob lexutil.Blob, blobBytes []byte) (*MicroNswfImgResp, error) {

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
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("micro-NSFW-img request failed: %v", err)
	}
	defer res.Body.Close()

	resp, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read micro-NSFW-img resp body: %v", err)
	}

	var nsfwScore MicroNswfImgResp
	if err = json.Unmarshal(resp, &nsfwScore); err != nil {
		return nil, fmt.Errorf("failed to parse micro-NSFW-img resp JSON: %v", err)
	}
	scoreJson, _ := json.Marshal(nsfwScore)
	log.Infof("micro-NSFW-img result cid=%s scores=%v", blob.Cid, string(scoreJson))
	return &nsfwScore, nil
}
