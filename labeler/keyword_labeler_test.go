package labeler

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

	desc := "yadda yadda"
	descRude := "yadda yadda 🍆"
	name := "Robyn Hood"
	nameSexy := "Sexy Robyn Hood"
	profileCases := []struct {
		record   bsky.ActorProfile
		expected []string
	}{
		{bsky.ActorProfile{DisplayName: &name}, []string{}},
		{bsky.ActorProfile{DisplayName: &name, Description: &desc}, []string{}},
		{bsky.ActorProfile{DisplayName: &name, Description: &descRude}, []string{"rude"}},
		{bsky.ActorProfile{DisplayName: &nameSexy}, []string{"rude"}},
		{bsky.ActorProfile{DisplayName: &nameSexy, Description: &descRude}, []string{"rude"}},
	}

	for _, c := range profileCases {
		vals := kl.LabelProfile(c.record)
		if !reflect.DeepEqual(vals, c.expected) {
			t.Log(fmt.Sprintf("labels expected:%s got:%s", c.expected, vals))
			t.Fail()
		}
	}
}
