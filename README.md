
![photo](https://static.bnewbold.net/tmp/indigo_serac.jpeg)

indigo: atproto libraries and services in golang
================================================

Some Bluesky software is developed in Typescript, and lives in the [bluesky-social/atproto](https://github.com/bluesky-social/atproto) repository. Some is developed in Go, and lives here.

Services implemented in this repository:

* **`bigsky`** ([README](./cmd/bigsky/README.md)): "Big Graph Service" (BGS) reference implementation, running at `bsky.network`
* **`palomar`** ([README](./cmd/palomar/README.md)): fulltext search service for <https://bsky.app>


## Development Quickstart

All the packages in this repository are under active development. Features and software interfaces have not stabilized and may break or be removed.

<p align="center"><img src="https://static.bnewbold.net/tmp/under_construction_bar.gif" /></p>

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

*not to be confused with the [AT command set](https://en.wikipedia.org/wiki/Hayes_command_set) or [Adenosine triphosphate](https://en.wikipedia.org/wiki/Adenosine_triphosphate)*

The Authenticated Transfer Protocol ("ATP" or "atproto") is a decentralized social media protocol, developed by [Bluesky PBC](https://blueskyweb.xyz). Learn more at:

- [Overview and Guides](https://atproto.com/guides/overview) 👈 Best starting point
- [Github Discussions](https://github.com/bluesky-social/atproto/discussions) 👈 Great place to ask questions
- [Protocol Specifications](https://atproto.com/specs/atp)
- [Blogpost on self-authenticating data structures](https://blueskyweb.xyz/blog/3-6-2022-a-self-authenticating-social-protocol)

The Bluesky Social application encompasses a set of schemas and APIs built in the overall AT Protocol framework. The namespace for these "Lexicons" is `app.bsky.*`.


## Contributions

We are working in the open, but not ready to actively collaborate on larger contributions to this codebase.

Please at least open an issue ahead of time, *before* starting any non-trivial work that you hope to get reviewed or merged to this repo.


## Are you a developer interested in building on atproto?

Bluesky is an open social network built on the AT Protocol, a flexible technology that will never lock developers out of the ecosystems that they help build. With atproto, third-party can be as seamless as first-party through custom feeds, federated services, clients, and more.

If you're a developer interested in building on atproto, we'd love to email you a Bluesky invite code. Simply share your GitHub (or similar) profile with us via [this form](https://forms.gle/BF21oxVNZiDjDhXF9).
