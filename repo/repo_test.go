package repo

import (
	"context"
	"fmt"
	"os"
	"testing"

	cid "github.com/ipfs/go-cid"
)

func TestRepo(t *testing.T) {
	t.Skip()
	fi, err := os.Open("repo.car")
	if err != nil {
		t.Fatal(err)
	}
	defer fi.Close()

	ctx := context.TODO()
	r, err := ReadRepoFromCar(ctx, fi)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.ForEach(ctx, "app.bsky.feed.post", func(k string, v cid.Cid) error {
		fmt.Println(k, v)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
