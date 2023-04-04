package labeler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/version"
)

type HiveAILabeler struct {
	Client   http.Client
	ApiToken string
}

// schema: https://docs.thehive.ai/reference/classification
type HiveAIResp struct {
	Status []HiveAIResp_Status `json:"status"`
}

type HiveAIResp_Status struct {
	Response HiveAIResp_Response `json:"response"`
}

type HiveAIResp_Response struct {
	Output []HiveAIResp_Out `json:"output"`
}

type HiveAIResp_Out struct {
	Time    float64            `json:"time"`
	Classes []HiveAIResp_Class `json:"classes"`
}

type HiveAIResp_Class struct {
	Class string  `json:"class"`
	Score float64 `json:"score"`
}

func NewHiveAILabeler(token string) HiveAILabeler {
	return HiveAILabeler{
		Client:   *util.RobustHTTPClient(),
		ApiToken: token,
	}
}

func (resp *HiveAIResp) SummarizeLabels() []string {
	var labels []string

	for _, status := range resp.Status {
		for _, out := range status.Response.Output {
			for _, cls := range out.Classes {
				// TODO(bnewbold): lots more upstream tags could be included here.
				// for example, "sexy" for not nude but still explicit/suggestive,
				// or lolicon (animated, not nude, "sugggestive"

				// sexual: https://docs.thehive.ai/docs/sexual-content
				// note: won't apply "nude" if "porn" already applied
				if cls.Class == "yes_sexual_activity" && cls.Score >= 0.90 {
					// NOTE: will include "hentai"
					labels = append(labels, "porn")
				} else if cls.Class == "animal_genitalia_and_human" && cls.Score >= 0.90 {
					labels = append(labels, "porn")
				} else if cls.Class == "yes_male_nudity" && cls.Score >= 0.90 {
					labels = append(labels, "nude")
				} else if cls.Class == "yes_female_nudity" && cls.Score >= 0.90 {
					labels = append(labels, "nude")
				}

				// gore and violence: https://docs.thehive.ai/docs/class-descriptions-violence-gore
				if cls.Class == "very_bloody" && cls.Score >= 0.90 {
					labels = append(labels, "gore")
				}
				if cls.Class == "human_corpse" && cls.Score >= 0.90 {
					labels = append(labels, "corpse")
				}
				if cls.Class == "yes_self_harm" && cls.Score >= 0.90 {
					labels = append(labels, "self-harm")
				}
			}
		}
	}

	return labels
}

func (hal *HiveAILabeler) LabelBlob(ctx context.Context, blob lexutil.LexBlob, blobBytes []byte) ([]string, error) {

	log.Infof("sending blob to thehive.ai cid=%s mimetype=%s size=%d", blob.Ref, blob.MimeType, len(blobBytes))

	// generic HTTP form file upload, then parse the response JSON
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("media", blob.Ref.String())
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

	req, err := http.NewRequest("POST", "https://api.thehive.ai/api/v2/task/sync", body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Token %s", hal.ApiToken))
	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "labelmaker/"+version.Version)

	res, err := hal.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HiveAI request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HiveAI request failed  statusCode=%d", res.StatusCode)
	}

	respBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read HiveAI resp body: %v", err)
	}

	log.Debugf("HiveAI raw result cid=%s body=%v", blob.Ref, string(respBytes))

	var respObj HiveAIResp
	if err := json.Unmarshal(respBytes, &respObj); err != nil {
		return nil, fmt.Errorf("failed to parse HiveAI resp JSON: %v", err)
	}
	respJson, _ := json.Marshal(respObj.Status[0].Response.Output[0])
	log.Infof("HiveAI result cid=%s json=%v", blob.Ref, string(respJson))
	return respObj.SummarizeLabels(), nil
}
