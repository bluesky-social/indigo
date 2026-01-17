package env

import (
	"fmt"
	"net/http"
)

const unset = "unset"

var Version = unset

func VersionHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "%s\n", Version) // nolint:errcheck
}

func IsProd() bool {
	return Version != unset
}
