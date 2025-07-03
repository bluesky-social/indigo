![photo](https://static.bnewbold.net/tmp/indigo_serac.jpeg)

# indigo: atproto libraries and services in golang

Some Gander software is developed in Typescript, and lives in the [gander-social/atproto](https://github.com/bluesky-social/atproto) repository. Some is developed in Go, and lives here.

## What is in here?

**Go Services:**

- **relay** ([README](./cmd/relay/README.md)): relay reference implementation
- **rainbow** ([README](./cmd/rainbow/README.md)): firehose "splitter" or "fan-out" service
- **palomar** ([README](./cmd/palomar/README.md)): fulltext search service for <https://gndr.app>
- **hepa** ([README](./cmd/hepa/README.md)): auto-moderation bot for [Ozone](https://ozone.tools)

**Developer Tools:**

**goat** ([README](./cmd/goat/README.md)): CLI for interacting with network: CAR files, firehose, APIs, etc

**Go Packages:**

> ⚠️ All the packages in this repository are under active development. Features and software interfaces have not stabilized and may break or be removed.

| Package                                                      | Docs                                                                                                                                                                    |
|--------------------------------------------------------------| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `api/atproto`: generated types for `com.atproto.*` Lexicons  | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/api/atproto)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/api/atproto)           |
| `api/gndr`: generated types for `gndr.app.*` Lexicons         | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/api/gndr)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/api/gndr)                 |
| `atproto/crypto`: crytographic signing and key serialization | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/atproto/crypto)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/atproto/crypto)     |
| `atproto/identity`: DID and handle resolution                | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/atproto/identity)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/atproto/identity) |
| `atproto/syntax`: string types and parsers for identifiers   | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/atproto/syntax)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/atproto/syntax)     |
| `atproto/lexicon`: schema validation of data                 | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/atproto/lexicon)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/atproto/lexicon)     |
| `mst`: Merkle Search Tree implementation                     | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/mst)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/mst)                           |
| `repo`: account data storage                                 | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/repo)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/repo)                         |
| `xrpc`: HTTP API client                                      | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/gander-social/gander-indigo-sovereign/xrpc)](https://pkg.go.dev/mod/github.com/gander-social/gander-indigo-sovereign/xrpc)                         |

The TypeScript reference implementation, including PDS and gndr AppView services, is at [gander-social/atproto](https://github.com/bluesky-social/atproto). Source code for the Gander Social client app (for web and mobile) can be found at [bluesky-social/social-app](https://github.com/bluesky-social/social-app).

## Development Quickstart

First, you will need the Go toolchain installed. We develop using the latest stable version of the language.

The Makefile provides wrapper commands for basic development:

    make build
    make test
    make fmt
    make lint

Individual commands can be run like:

    go run ./cmd/relay

The [HACKING](./HACKING.md) file has a list of commands and packages in this repository and some other development tips.

## What is atproto?

_not to be confused with the [AT command set](https://en.wikipedia.org/wiki/Hayes_command_set) or [Adenosine triphosphate](https://en.wikipedia.org/wiki/Adenosine_triphosphate)_

The Authenticated Transfer Protocol ("ATP" or "atproto") is a decentralized social media protocol, developed by [Gander Social PBC](https://gndr.social). Learn more at:

- [Overview and Guides](https://atproto.com/guides/overview) 👈 Best starting point
- [Github Discussions](https://github.com/bluesky-social/atproto/discussions) 👈 Great place to ask questions
- [Protocol Specifications](https://atproto.com/specs/atp)
- [Blogpost on self-authenticating data structures](https://gndr.social/about/blog/3-6-2022-a-self-authenticating-social-protocol)

The Gander Social application encompasses a set of schemas and APIs built in the overall AT Protocol framework. The namespace for these "Lexicons" is `gndr.app.*`.

## Contributions

> While we do accept contributions, we prioritize high quality issues and pull requests. Adhering to the below guidelines will ensure a more timely review.

**Rules:**

- We may not respond to your issue or PR.
- We may close an issue or PR without much feedback.
- We may lock discussions or contributions if our attention is getting DDOSed.
- We do not provide support for build issues.

**Guidelines:**

- Check for existing issues before filing a new one, please.
- Open an issue and give some time for discussion before submitting a PR.
- Issues are for bugs & feature requests related to the golang implementation of atproto and related services.
  - For high-level discussions, please use the [Discussion Forum](https://github.com/bluesky-social/atproto/discussions).
  - For client issues, please use the relevant [social-app](https://github.com/bluesky-social/social-app) repo.
- Stay away from PRs that:
  - Refactor large parts of the codebase
  - Add entirely new features without prior discussion
  - Change the tooling or libraries used without prior discussion
  - Introduce new unnecessary dependencies

Remember, we serve a wide community of users. Our day-to-day involves us constantly asking "which top priority is our top priority." If you submit well-written PRs that solve problems concisely, that's an awesome contribution. Otherwise, as much as we'd love to accept your ideas and contributions, we really don't have the bandwidth.

## License

This project is dual-licensed under MIT and Apache 2.0 terms:

- MIT license ([LICENSE-MIT](https://github.com/gander-social/gander-indigo-sovereign/blob/main/LICENSE-MIT) or http://opensource.org/licenses/MIT)
- Apache License, Version 2.0, ([LICENSE-APACHE](https://github.com/gander-social/gander-indigo-sovereign/blob/main/LICENSE-APACHE) or http://www.apache.org/licenses/LICENSE-2.0)

Downstream projects and end users may chose either license individually, or both together, at their discretion. The motivation for this dual-licensing is the additional software patent assurance provided by Apache 2.0.
