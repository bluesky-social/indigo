package lexgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"golang.org/x/tools/imports"
)

// Compile and exercise the generated types, rather than asserting on generator source text.
func TestGeneratedJSONBehavior(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	module := fmt.Sprintf("module generated.test\n\ngo 1.26\n\nrequire github.com/bluesky-social/indigo v0.0.0\nreplace github.com/bluesky-social/indigo => %s\n", filepath.ToSlash(root))
	writeGeneratedTestFile(t, filepath.Join(dir, "go.mod"), []byte(module))
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeGeneratedTestFile(t, filepath.Join(dir, "go.sum"), sum)

	catalog := lexicon.NewBaseCatalog()
	var schemas []*lexicon.SchemaFile
	for _, source := range []string{generatedFixtureSchema, generatedSharedSchema, generatedRemoteSchema, generatedQuerySchema, generatedSpaceSchema} {
		var schema lexicon.SchemaFile
		if err := json.Unmarshal([]byte(source), &schema); err != nil {
			t.Fatal(err)
		}
		if err := catalog.AddSchemaFile(schema); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, &schema)
	}
	config := NewGenConfig()
	config.OutputPackage = "wire"
	config.OutputImport = "generated.test/wire"
	config.UnknownType = "json-raw-message"
	config.PackageMappings = map[string]string{
		"com.atproto.generator":        "generated.test/wire",
		"org.example.shared":           "generated.test/wire",
		"com.atproto.generator.remote": "generated.test/remote",
	}
	for _, schema := range schemas {
		fileConfig := *config
		if strings.HasSuffix(schema.ID, ".remote") {
			fileConfig.OutputPackage = "remote"
			fileConfig.OutputImport = "generated.test/remote"
		}
		generateBehaviorFile(t, dir, schema, catalog, &fileConfig)
	}
	legacy := LegacyConfig()
	legacy.OutputPackage = "legacy"
	legacy.OutputImport = "generated.test/legacy"
	generateBehaviorFile(t, dir, schemas[3], catalog, legacy)
	writeGeneratedTestFile(t, filepath.Join(dir, "wire", "behavior_test.go"), []byte(generatedBehaviorTests))
	writeGeneratedTestFile(t, filepath.Join(dir, "legacy", "behavior_test.go"), []byte(generatedLegacyTests))

	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated-code behavior tests: %v\n%s", err, output)
	}
}

func generateBehaviorFile(t *testing.T, dir string, schema *lexicon.SchemaFile, catalog lexicon.Catalog, config *GenConfig) {
	t.Helper()
	flat, err := FlattenSchemaFile(schema)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gen := CodeGenerator{Config: config, Lex: flat, Cat: catalog, Out: &buf}
	if err := gen.WriteLexicon(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, config.OutputPackage, gen.FileName())
	output, err := imports.Process(path, buf.Bytes(), &imports.Options{Comments: true, TabIndent: true, TabWidth: 8})
	if err != nil {
		t.Fatal(err)
	}
	writeGeneratedTestFile(t, path, output)
}

func writeGeneratedTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
}

const generatedFixtureSchema = `{
  "lexicon": 1, "id": "com.atproto.generator.fixture", "defs": {
    "main": {"type":"object", "required":["value", "shared", "remote"], "properties": {
      "open": {"type":"union", "refs":["#first", "#second"]},
      "closed": {"type":"union", "closed":true, "refs":["#first", "#second"]},
      "value": {"type":"unknown"}, "optionalValue": {"type":"unknown"},
      "shared": {"type":"ref", "ref":"org.example.shared.defs#entry"},
      "remote": {"type":"ref", "ref":"com.atproto.generator.remote"}
    }},
    "first": {"type":"object", "required":["count"], "properties":{"count":{"type":"integer"}}},
    "second": {"type":"object", "required":["text"], "properties":{"text":{"type":"string"}}}
  }
}`
const generatedSharedSchema = `{"lexicon":1,"id":"org.example.shared.defs","defs":{"entry":{"type":"object","properties":{"name":{"type":"string"}}}}}`
const generatedRemoteSchema = `{"lexicon":1,"id":"com.atproto.generator.remote","defs":{"main":{"type":"object","properties":{"name":{"type":"string"}}}}}`
const generatedQuerySchema = `{"lexicon":1,"id":"com.atproto.generator.query","defs":{"main":{"type":"query","parameters":{"type":"params","required":["repo"],"properties":{"cursor":{"type":"string"},"flag":{"type":"boolean"},"limit":{"type":"integer"},"repo":{"type":"string"}}},"output":{"encoding":"application/json","schema":{"type":"object","required":["value"],"properties":{"value":{"type":"unknown"}}}}}}}`
const generatedSpaceSchema = `{"lexicon":1,"id":"com.atproto.generator.space","defs":{"main":{"type":"space","key":"any","name":"Test space","collections":["com.atproto.generator.fixture"]}}}`

const generatedBehaviorTests = `package wire

import (
  "bytes"
  "context"
  "encoding/json"
  "reflect"
  "testing"

  "github.com/bluesky-social/indigo/lex/util"
)

func TestOpenUnionRoundTripAndReuse(t *testing.T) {
  first := []byte("{\"$type\":\"com.atproto.generator.fixture#first\",\"count\":7}")
  second := []byte("{\"$type\":\"com.atproto.generator.fixture#second\",\"text\":\"hi\"}")
  unknown := []byte("{\"$type\":\"org.future.policy\",\"large\":9007199254740993,\"nested\":{\"x\":[true,null,3]}}")
  var value GeneratorFixture_Open
  for _, source := range [][]byte{first, second, unknown, first} {
    if err := value.UnmarshalJSON(source); err != nil { t.Fatal(err) }
    result, err := json.Marshal(&value)
    if err != nil { t.Fatal(err) }
    if !bytes.Equal(result, source) { t.Fatalf("roundtrip: %s != %s", result, source) }
    result, err = json.Marshal(value)
    if err != nil || !bytes.Equal(result, source) { t.Fatalf("value-held roundtrip: %s (%v)", result, err) }
    nested, err := json.Marshal(struct { Policy GeneratorFixture_Open ` + "`json:\"policy\"`" + ` }{Policy: value})
    if err != nil || !bytes.Contains(nested, source) { t.Fatalf("required field roundtrip: %s (%v)", nested, err) }
    switch {
    case bytes.Equal(source, first):
      if value.GeneratorFixture_First == nil || value.GeneratorFixture_Second != nil || value.Unknown != nil { t.Fatal("stale state after first") }
    case bytes.Equal(source, second):
      if value.GeneratorFixture_First != nil || value.GeneratorFixture_Second == nil || value.Unknown != nil { t.Fatal("stale state after second") }
    default:
      if value.GeneratorFixture_First != nil || value.GeneratorFixture_Second != nil || !bytes.Equal(value.Unknown, source) { t.Fatal("unknown variant was lost") }
    }
  }
  if err := value.UnmarshalJSON(unknown); err != nil { t.Fatal(err) }
  unknown[0] = ' '
  if value.Unknown[0] != '{' { t.Fatal("unknown storage aliases input") }
}

func TestUnionErrorsClearDestination(t *testing.T) {
  known := []byte("{\"$type\":\"com.atproto.generator.fixture#first\",\"count\":7}")
  for _, source := range []string{ "{}", "null", "[]", "{", "{\"$type\":4}", "{\"$type\":\"com.atproto.generator.fixture#first\",\"count\":\"bad\"}" } {
    var open GeneratorFixture_Open
    if err := open.UnmarshalJSON(known); err != nil { t.Fatal(err) }
    if err := open.UnmarshalJSON([]byte(source)); err == nil { t.Fatalf("accepted %s", source) }
    if open.GeneratorFixture_First != nil || open.GeneratorFixture_Second != nil || open.Unknown != nil { t.Fatalf("retained state after %s", source) }
  }
  var closed GeneratorFixture_Closed
  if err := closed.UnmarshalJSON(known); err != nil { t.Fatal(err) }
  if err := closed.UnmarshalJSON([]byte("{\"$type\":\"org.future.policy\"}")); err == nil { t.Fatal("closed union accepted unknown type") }
  if closed.GeneratorFixture_First != nil || closed.GeneratorFixture_Second != nil { t.Fatal("closed union retained state") }
  if _, err := json.Marshal(&closed); err == nil { t.Fatal("empty closed union marshaled") }
  if _, err := json.Marshal(&GeneratorFixture_Open{}); err == nil { t.Fatal("empty open union marshaled") }
  if _, err := json.Marshal(&GeneratorFixture_Open{Unknown: json.RawMessage("{}")}); err == nil { t.Fatal("untyped unknown variant marshaled") }
}

func TestRawUnknownFieldsAndPackageReferences(t *testing.T) {
  source := []byte("{\"value\":{\"$type\":\"org.future.record\",\"n\":9007199254740993},\"shared\":{},\"remote\":{}}")
  var value GeneratorFixture
  if err := json.Unmarshal(source, &value); err != nil { t.Fatal(err) }
  var raw json.RawMessage = value.Value
  if !bytes.Equal(raw, []byte("{\"$type\":\"org.future.record\",\"n\":9007199254740993}")) { t.Fatalf("unknown data changed: %s", raw) }
  if value.OptionalValue != nil { t.Fatal("omitted unknown value became present") }
  value.Shared = SharedDefs_Entry{}
  result, err := json.Marshal(value)
  if err != nil { t.Fatal(err) }
  if !bytes.Contains(result, raw) { t.Fatalf("raw value did not roundtrip: %s", result) }
  if err := json.Unmarshal([]byte("{\"value\":{},\"optionalValue\":{\"n\":9007199254740993},\"shared\":{},\"remote\":{}}"), &value); err != nil { t.Fatal(err) }
  if value.OptionalValue == nil || !bytes.Equal(*value.OptionalValue, []byte("{\"n\":9007199254740993}")) { t.Fatal("optional unknown data changed") }
}

type captureClient struct { params map[string]interface{} }
func (c *captureClient) LexDo(_ context.Context, method string, encoding, endpoint string, params map[string]interface{}, input, output interface{}) error {
  if method != util.Query || endpoint != "com.atproto.generator.query" { panic("wrong RPC") }
  c.params = params
  return json.Unmarshal([]byte("{\"value\":{\"$type\":\"org.future.record\",\"n\":9007199254740993}}"), output)
}
func TestOptionalQueryParameters(t *testing.T) {
  client := &captureClient{}
  out, err := GeneratorQuery(context.Background(), client, nil, nil, nil, "did:web:example.com")
  if err != nil { t.Fatal(err) }
  if !reflect.DeepEqual(client.params, map[string]interface{}{"repo":"did:web:example.com"}) { t.Fatalf("omitted params: %#v", client.params) }
  if !bytes.Contains(out.Value, []byte("9007199254740993")) { t.Fatal("RPC raw value lost precision") }
  cursor, flag, limit := "", false, int64(0)
  if _, err := GeneratorQuery(context.Background(), client, &cursor, &flag, &limit, "did:web:example.com"); err != nil { t.Fatal(err) }
  want := map[string]interface{}{"cursor":"", "flag":false, "limit":int64(0), "repo":"did:web:example.com"}
  if !reflect.DeepEqual(client.params, want) { t.Fatalf("explicit zero params: %#v", client.params) }
}
`

const generatedLegacyTests = `package legacy

import (
 "context"
 "testing"
 "github.com/bluesky-social/indigo/lex/util"
)
type captureClient struct { params map[string]interface{} }
func (c *captureClient) LexDo(_ context.Context, method string, _, _ string, params map[string]interface{}, _, _ interface{}) error { if method != util.Query { panic("wrong RPC") }; c.params = params; return nil }
func TestLegacyOptionalParameters(t *testing.T) {
 c := &captureClient{}
 if _, err := GeneratorQuery(context.Background(), c, "", false, 0, "did:web:example.com"); err != nil { t.Fatal(err) }
 if len(c.params) != 1 { t.Fatalf("legacy zeros were included: %#v", c.params) }
 if _, err := GeneratorQuery(context.Background(), c, "page", true, 1, "did:web:example.com"); err != nil { t.Fatal(err) }
 if len(c.params) != 4 { t.Fatalf("legacy nonzero params missing: %#v", c.params) }
}
`
