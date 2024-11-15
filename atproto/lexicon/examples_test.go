package lexicon

import (
	"fmt"

	atdata "github.com/bluesky-social/indigo/atproto/data"
)

func ExampleValidateRecord() {

	// First load Lexicon schema JSON files from local disk.
	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		panic("failed to load lexicons")
	}

	// Parse record JSON data using atproto/data helper
	recordJSON := `{
		"$type": "example.lexicon.record",
		"integer": 123,
        "formats": {
            "did": "did:web:example.com",
            "aturi": "at://handle.example.com/com.example.nsid/asdf123",
            "datetime": "2023-10-30T22:25:23Z",
            "language": "en",
            "tid": "3kznmn7xqxl22"
        }
	}`

	recordData, err := atdata.UnmarshalJSON([]byte(recordJSON))
	if err != nil {
		panic("failed to parse record JSON")
	}

	if err := ValidateRecord(&cat, recordData, "example.lexicon.record", 0); err != nil {
		fmt.Printf("Schema validation failed: %v\n", err)
	} else {
		fmt.Println("Success!")
	}
	// Output: Success!
}
