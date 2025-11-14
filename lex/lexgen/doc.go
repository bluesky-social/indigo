/*
Package implementing Go code generation for lexicon schemas.

Used by the 'lexgen' CLI tool to output Go structs and client API helpers based on Lexicon schemas. This package currently includes a "legacy" mode to stay as close as possible to the previous code generation output.

WARNING: this package is still a work in progress. Both the package API and the generated code are likely to change, possibly in backwards-incompatible ways.

# Package Structure

The package works in two steps:

- "flattening" parses a full lexicon schema file and copies nested type definitions in to a top-level array
- code generation outputs a single Go source code file corresponding to a flattened lexicon schema file

Wrapping code is expected to handle code formatting and fixing imports (which mostly means removing unused imports).
*/
package lexgen
