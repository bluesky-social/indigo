package labeling

import (
	"fmt"
	"reflect"
	"testing"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

func TestKeywordFilter(t *testing.T) {
	var kl = KeywordLabeler{Value: "rude", Keywords: []string{"🍆", "sex"}}

	postCases := []struct {
		record   bsky.FeedPost
		expected []string
	}{
		{bsky.FeedPost{Text: "boring inoffensive tweet"}, []string{}},
		{bsky.FeedPost{Text: "I love Aubergine 🍆"}, []string{"rude"}},
		{bsky.FeedPost{Text: "SeXyTiMe"}, []string{"rude"}},
	}

	for _, c := range postCases {
		vals := kl.LabelPost(c.record)
		if !reflect.DeepEqual(vals, c.expected) {
			t.Log(fmt.Sprintf("labels expected:%s got:%s", c.expected, vals))
			t.Fail()
		}
	}

	var desc = "yadda yadda"
	var descRude = "yadda yadda 🍆"
	profileCases := []struct {
		record   bsky.ActorProfile
		expected []string
	}{
		{bsky.ActorProfile{DisplayName: "Robyn Hood"}, []string{}},
		{bsky.ActorProfile{DisplayName: "Robyn Hood", Description: &desc}, []string{}},
		{bsky.ActorProfile{DisplayName: "Robyn Hood", Description: &descRude}, []string{"rude"}},
		{bsky.ActorProfile{DisplayName: "Sexy Robyn Hood"}, []string{"rude"}},
	}

	for _, c := range profileCases {
		vals := kl.LabelProfile(c.record)
		if !reflect.DeepEqual(vals, c.expected) {
			t.Log(fmt.Sprintf("labels expected:%s got:%s", c.expected, vals))
			t.Fail()
		}
	}
}
