package labeling

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/bluesky-social/indigo/api"
	bskyapi "github.com/bluesky-social/indigo/api/bsky"
)

func TestKeywordFilter(t *testing.T) {
	var kl = KeywordLabeler{value: "rude", keywords: []string{"🍆", "sex"}}

	postCases := []struct {
		record   api.PostRecord
		expected []string
	}{
		{api.PostRecord{Text: "boring inoffensive tweet"}, []string{}},
		{api.PostRecord{Text: "I love Aubergine 🍆"}, []string{"rude"}},
		{api.PostRecord{Text: "SeXyTiMe"}, []string{"rude"}},
	}

	for _, c := range postCases {
		vals := kl.labelPost(c.record)
		if !reflect.DeepEqual(vals, c.expected) {
			t.Log(fmt.Sprintf("labels expected:%s got:%s", c.expected, vals))
			t.Fail()
		}
	}

	var desc = "yadda yadda"
	var descRude = "yadda yadda 🍆"
	profileCases := []struct {
		record   bskyapi.ActorProfile
		expected []string
	}{
		{bskyapi.ActorProfile{DisplayName: "Robyn Hood"}, []string{}},
		{bskyapi.ActorProfile{DisplayName: "Robyn Hood", Description: &desc}, []string{}},
		{bskyapi.ActorProfile{DisplayName: "Robyn Hood", Description: &descRude}, []string{"rude"}},
		{bskyapi.ActorProfile{DisplayName: "Sexy Robyn Hood"}, []string{"rude"}},
	}

	for _, c := range profileCases {
		vals := kl.labelActorProfile(c.record)
		if !reflect.DeepEqual(vals, c.expected) {
			t.Log(fmt.Sprintf("labels expected:%s got:%s", c.expected, vals))
			t.Fail()
		}
	}
}
