package visual

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"

	"github.com/carlmjohnson/versioninfo"
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

// Simple direct mappings from individual classes to to labels
//
// hive gore and violence: https://docs.thehive.ai/docs/class-descriptions-violence-gore
func summarizeSimpleLabels(cl []HiveAIResp_Class) []string {
	var labels []string

	for _, cls := range cl {
		if cls.Class == "very_bloody" && cls.Score >= 0.90 {
			labels = append(labels, "gore")
		}
		if cls.Class == "human_corpse" && cls.Score >= 0.90 {
			labels = append(labels, "corpse")
		}
		if cls.Class == "hanging" && cls.Score >= 0.90 {
			labels = append(labels, "corpse")
		}
		if cls.Class == "yes_self_harm" && cls.Score >= 0.96 {
			labels = append(labels, "self-harm")
		}
	}
	return labels
}

// Matches only one (or none) of: porn, sexual, nudity
//
// porn: sexual and nudity. including both explicit activity or full-frontal and suggestive/intent
// sexual: sexually suggestive, not explicit; may include some forms of nudity
// nudity: non-sexual nudity (eg, artistic, possibly some photographic)
//
// hive docs/definitions: https://docs.thehive.ai/docs/sexual-content
func summarizeSexualLabels(cl []HiveAIResp_Class) string {

	scores := make(map[string]float64)
	for _, cls := range cl {
		scores[cls.Class] = cls.Score
	}

	// first check if porn...
	for _, pornClass := range []string{"yes_sexual_activity", "animal_genitalia_and_human", "yes_realistic_nsfw"} {
		if scores[pornClass] >= 0.9 {
			return "porn"
		}
	}
	if scores["general_nsfw"] >= 0.9 {
		// special case for some anime examples
		if scores["animated_animal_genitalia"] >= 0.5 {
			return "porn"
		}

		// special case for some pornographic/explicit classic drawings
		if scores["yes_undressed"] >= 0.9 && scores["yes_sexual_activity"] >= 0.9 {
			return "porn"
		}
	}

	// then check for sexual suggestive (which may include nudity)...
	for _, sexualClass := range []string{"yes_sexual_intent", "yes_sex_toy"} {
		if scores[sexualClass] >= 0.9 {
			return "sexual"
		}
	}
	if scores["yes_undressed"] >= 0.9 {
		// special case for bondage examples
		if scores["yes_sex_toy"] > 0.75 {
			return "sexual"
		}
	}

	// then non-sexual nudity...
	for _, nudityClass := range []string{"yes_male_nudity", "yes_female_nudity", "yes_undressed"} {
		if scores[nudityClass] >= 0.9 {
			return "nudity"
		}
	}

	// then finally flag remaining "underwear" images in to sexually suggestive
	// (after non-sexual content already labeled above)
	for _, underwearClass := range []string{"yes_male_underwear", "yes_female_underwear"} {
		if scores[underwearClass] >= 0.9 {
			return "sexual"
		}
	}

	return ""
}

func (resp *HiveAIResp) SummarizeLabels() []string {
	var labels []string

	for _, status := range resp.Status {
		for _, out := range status.Response.Output {
			simple := summarizeSimpleLabels(out.Classes)
			if len(simple) > 0 {
				labels = append(labels, simple...)
			}

			sexual := summarizeSexualLabels(out.Classes)
			if sexual != "" {
				labels = append(labels, sexual)
			}
		}
	}

	return labels
}

func (hal *HiveAILabeler) LabelBlob(ctx context.Context, blob lexutil.LexBlob, blobBytes []byte) ([]string, error) {

	slog.Info("sending blob to thehive.ai", "cid", blob.Ref, "mimetype", blob.MimeType, "size", len(blobBytes))

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
	req.Header.Set("User-Agent", "labelmaker/"+versioninfo.Short())

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

	slog.Debug("HiveAI raw result", "cid", blob.Ref, "body", string(respBytes))

	var respObj HiveAIResp
	if err := json.Unmarshal(respBytes, &respObj); err != nil {
		return nil, fmt.Errorf("failed to parse HiveAI resp JSON: %v", err)
	}
	respJson, _ := json.Marshal(respObj.Status[0].Response.Output[0])
	slog.Info("HiveAI result", "cid", blob.Ref, "json", string(respJson))
	return respObj.SummarizeLabels(), nil
}
