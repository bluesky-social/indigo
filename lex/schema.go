package lex

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Schema is a lexicon json file
// e.g. atproto/lexicons/app/bsky/feed/post.json
// https://atproto.com/specs/lexicon
type Schema struct {
	// path of json file read
	path string

	// prefix of lexicon group, e.g. "app.bsky" or "com.atproto"
	prefix string

	// Lexicon version, e.g. 1
	Lexicon int                    `json:"lexicon"`
	ID      string                 `json:"id"`
	Defs    map[string]*TypeSchema `json:"defs"`
}

func ReadSchema(f string) (*Schema, error) {
	fi, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	var s Schema
	if err := json.NewDecoder(fi).Decode(&s); err != nil {
		return nil, err
	}
	s.path = f

	return &s, nil
}

func (s *Schema) Name() string {
	p := strings.Split(s.ID, ".")
	return p[len(p)-2] + p[len(p)-1]
}

func (s *Schema) AllTypes(prefix string, defMap map[string]*ExtDef) []outputType {
	var out []outputType

	var walk func(name string, ts *TypeSchema, needsCbor bool)
	walk = func(name string, ts *TypeSchema, needsCbor bool) {
		if ts == nil {
			panic(fmt.Sprintf("nil type schema in %q (%s)", name, s.ID))
		}

		if needsCbor {
			fmt.Println("Setting to record: ", name)
			if name == "EmbedImages_View" {
				panic("not ok")
			}
			ts.needsCbor = true
		}

		if name == "LabelDefs_SelfLabels" {
			ts.needsType = true
		}

		ts.prefix = prefix
		ts.id = s.ID
		ts.defMap = defMap
		if ts.Type == "object" ||
			(ts.Type == "union" && len(ts.Refs) > 0) {
			out = append(out, outputType{
				Name:      name,
				Type:      ts,
				NeedsCbor: ts.needsCbor,
			})

			for _, r := range ts.Refs {
				refname := r
				if strings.HasPrefix(refname, "#") {
					refname = s.ID + r
				}

				ed, ok := defMap[refname]
				if !ok {
					panic(fmt.Sprintf("cannot find: %q", refname))
				}

				fmt.Println("UNION REF", refname, name, needsCbor)

				if needsCbor {
					ed.Type.needsCbor = true
				}

				ed.Type.needsType = true
			}
		}

		if ts.Type == "ref" {
			refname := ts.Ref
			if strings.HasPrefix(refname, "#") {
				refname = s.ID + ts.Ref
			}

			sub, ok := defMap[refname]
			if !ok {
				panic(fmt.Sprintf("missing ref: %q", refname))
			}

			if needsCbor {
				sub.Type.needsCbor = true
			}
		}

		for childname, val := range ts.Properties {
			walk(name+"_"+strings.Title(childname), val, ts.needsCbor)
		}

		if ts.Items != nil {
			walk(name+"_Elem", ts.Items, ts.needsCbor)
		}

		if ts.Input != nil {
			if ts.Input.Schema == nil {
				if ts.Input.Encoding != EncodingCBOR &&
					ts.Input.Encoding != EncodingANY &&
					ts.Input.Encoding != EncodingCAR &&
					ts.Input.Encoding != EncodingMP4 {
					panic(fmt.Sprintf("strange input type def in %s", s.ID))
				}
			} else {
				walk(name+"_Input", ts.Input.Schema, ts.needsCbor)
			}
		}

		if ts.Output != nil {
			if ts.Output.Schema == nil {
				if ts.Output.Encoding != EncodingCBOR &&
					ts.Output.Encoding != EncodingCAR &&
					ts.Output.Encoding != EncodingANY &&
					ts.Output.Encoding != EncodingJSONL &&
					ts.Output.Encoding != EncodingMP4 {
					panic(fmt.Sprintf("strange output type def in %s", s.ID))
				}
			} else {
				walk(name+"_Output", ts.Output.Schema, ts.needsCbor)
			}
		}

		if ts.Type == "record" {
			ts.Record.needsType = true
			walk(name, ts.Record, true)
		}

	}

	tname := nameFromID(s.ID, prefix)

	for name, def := range s.Defs {
		n := tname + "_" + strings.Title(name)
		if name == "main" {
			n = tname
		}
		walk(n, def, def.needsCbor)
	}

	return out
}
