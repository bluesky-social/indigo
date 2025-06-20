package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gander-social/gander-indigo-sovereign/mst"
	"github.com/gander-social/gander-indigo-sovereign/repo"
	"github.com/gander-social/gander-indigo-sovereign/util"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/xlab/treeprint"
)

func prettyMST(ctx context.Context, carFile io.Reader, opts repoMSTOptions) error {

	// read repository tree in to memory
	r, err := repo.ReadRepoFromCar(ctx, carFile)
	if err != nil {
		return err
	}
	cst := util.CborStore(r.Blockstore())
	// determine which root cid to use, defaulting to repo data root
	rootCID := r.DataCid()
	if opts.root != "" {
		optsRootCID, err := cid.Decode(opts.root)
		if err != nil {
			return err
		}
		rootCID = optsRootCID
	}
	// start walking mst
	exists, err := nodeExists(ctx, cst, rootCID)
	if err != nil {
		return err
	}
	tree := treeprint.NewWithRoot(displayCID(&rootCID, exists, opts))
	if exists {
		if err := walkMST(ctx, cst, rootCID, tree, opts); err != nil {
			return err
		}
	}
	// print tree
	fmt.Println(tree.String())
	return nil
}

func walkMST(ctx context.Context, cst *cbor.BasicIpldStore, cid cid.Cid, tree treeprint.Tree, opts repoMSTOptions) error {
	var node mst.NodeData
	if err := cst.Get(ctx, cid, &node); err != nil {
		return err
	}
	if node.Left != nil {
		exists, err := nodeExists(ctx, cst, *node.Left)
		if err != nil {
			return err
		}
		subtree := tree.AddBranch(displayCID(node.Left, exists, opts))
		if exists {
			if err := walkMST(ctx, cst, *node.Left, subtree, opts); err != nil {
				return err
			}
		}
	}
	for _, entry := range node.Entries {
		exists, err := nodeExists(ctx, cst, entry.Val)
		if err != nil {
			return err
		}
		tree.AddNode(displayEntryVal(&entry, exists, opts))
		if entry.Tree != nil {
			exists, err := nodeExists(ctx, cst, *entry.Tree)
			if err != nil {
				return err
			}
			subtree := tree.AddBranch(displayCID(entry.Tree, exists, opts))
			if exists {
				if err := walkMST(ctx, cst, *entry.Tree, subtree, opts); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func displayEntryVal(entry *mst.TreeEntry, exists bool, opts repoMSTOptions) string {
	key := string(entry.KeySuffix)
	divider := " "
	if opts.fullCID {
		divider = "\n"
	}
	return strings.Repeat("∙", int(entry.PrefixLen)) + key + divider + displayCID(&entry.Val, exists, opts)
}

func displayCID(cid *cid.Cid, exists bool, opts repoMSTOptions) string {
	cidDisplay := cid.String()
	if !opts.fullCID {
		cidDisplay = "…" + string(cidDisplay[len(cidDisplay)-7:])
	}
	connector := "─◉"
	if !exists {
		connector = "─◌"
	}
	return "[" + cidDisplay + "]" + connector
}

type repoMSTOptions struct {
	carPath string
	fullCID bool
	root    string
}

func nodeExists(ctx context.Context, cst *cbor.BasicIpldStore, cid cid.Cid) (bool, error) {
	if _, err := cst.Blocks.Get(ctx, cid); err != nil {
		if errors.Is(err, ipld.ErrNotFound{}) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
