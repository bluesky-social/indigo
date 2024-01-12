package visual

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"
)

func TestHiveParse(t *testing.T) {
	file, err := os.Open("testdata/hiveai_resp_example.json")
	if err != nil {
		t.Fatal(err)
	}

	respBytes, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	var respObj HiveAIResp
	if err := json.Unmarshal(respBytes, &respObj); err != nil {
		t.Fatal(err)
	}

	classes := respObj.Status[0].Response.Output[0].Classes
	if len(classes) <= 10 {
		t.Fatal("didn't get expected class count")
	}
	for _, c := range classes {
		if c.Class == "" || c.Score == 0.0 {
			t.Fatal("got null/empty class in resp")
		}
	}

	labels := respObj.SummarizeLabels()
	expected := []string{"porn"}
	if !reflect.DeepEqual(labels, expected) {
		t.Fatal("didn't summarize to expected labels")
	}
}
