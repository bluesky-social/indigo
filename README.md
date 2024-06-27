![photo](https://static.bnewbold.net/tmp/indigo_serac.jpeg)

# indigo: atproto libraries and services in golang

Some Bluesky software is developed in Typescript, and lives in the [bluesky-social/atproto](https://github.com/bluesky-social/atproto) repository. Some is developed in Go, and lives here.

## What is in here?

**Go Services:**

- **bigsky** ([README](./cmd/bigsky/README.md)): "Big Graph Service" (BGS) reference implementation, running at `bsky.network`
- **palomar** ([README](./cmd/palomar/README.md)): fulltext search service for <https://bsky.app>
- **hepa** ([README](./cmd/hepa/README.md)): auto-moderation bot for [Ozone](https://ozone.tools)

**Go Packages:**

> ⚠️ All the packages in this repository are under active development. Features and software interfaces have not stabilized and may break or be removed.

| Package                                                      | Docs                                                                                                                                                                    |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `api/atproto`: generated types for `com.atproto.*` Lexicons  | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/api/atproto)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/api/atproto)           |
| `api/bsky`: generated types for `app.bsky.*` Lexicons        | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/api/bsky)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/api/bsky)                 |
| `atproto/crypto`: crytographic signing and key serialization | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/crypto)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/crypto)     |
| `atproto/identity`: DID and handle resolution                | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/identity)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/identity) |
| `atproto/syntax`: string types and parsers for identifiers   | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/syntax)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/syntax)     |
| `mst`: Merkle Search Tree implementation                     | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/mst)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/mst)                           |
| `repo`: account data storage                                 | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/repo)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/repo)                         |
| `xrpc`: HTTP API client                                      | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/xrpc)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/xrpc)                         |

The TypeScript reference implementation, including PDS and bsky AppView services, is at [bluesky-social/atproto](https://github.com/bluesky-social/atproto). Source code for the Bluesky Social client app (for web and mobile) can be found at [bluesky-social/social-app](https://github.com/bluesky-social/social-app).

## Development Quickstart

First, you will need the Go toolchain installed. We develop using the latest stable version of the language.

The Makefile provides wrapper commands for basic development:

    make build
    make test
    make fmt
    make lint

Individual commands can be run like:

    go run ./cmd/bigsky

The [HACKING](./HACKING.md) file has a list of commands and packages in this repository and some other development tips.

## What is atproto?

_not to be confused with the [AT command set](https://en.wikipedia.org/wiki/Hayes_command_set) or [Adenosine triphosphate](https://en.wikipedia.org/wiki/Adenosine_triphosphate)_

The Authenticated Transfer Protocol ("ATP" or "atproto") is a decentralized social media protocol, developed by [Bluesky PBC](https://bsky.social). Learn more at:

- [Overview and Guides](https://atproto.com/guides/overview) 👈 Best starting point
- [Github Discussions](https://github.com/bluesky-social/atproto/discussions) 👈 Great place to ask questions
- [Protocol Specifications](https://atproto.com/specs/atp)
- [Blogpost on self-authenticating data structures](https://bsky.social/about/blog/3-6-2022-a-self-authenticating-social-protocol)

The Bluesky Social application encompasses a set of schemas and APIs built in the overall AT Protocol framework. The namespace for these "Lexicons" is `app.bsky.*`.

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

## Are you a developer interested in building on atproto?

Bluesky is an open social network built on the AT Protocol, a flexible technology that will never lock developers out of the ecosystems that they help build. With atproto, third-party can be as seamless as first-party through custom feeds, federated services, clients, and more.

## License

This project is dual-licensed under MIT and Apache 2.0 terms:

- MIT license ([LICENSE-MIT](https://github.com/bluesky-social/indigo/blob/main/LICENSE-MIT) or http://opensource.org/licenses/MIT)
- Apache License, Version 2.0, ([LICENSE-APACHE](https://github.com/bluesky-social/indigo/blob/main/LICENSE-APACHE) or http://www.apache.org/licenses/LICENSE-2.0)

Downstream projects and end users may chose either license individually, or both together, at their discretion. The motivation for this dual-licensing is the additional software patent assurance provided by Apache 2.0.
