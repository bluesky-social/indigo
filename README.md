
![photo](https://static.bnewbold.net/tmp/indigo_serac.jpeg)

indigo: golang code for Bluesky's atproto services
==================================================

Some Bluesky software is developed in Typescript, and lives in the [bluesky-social/atproto](https://github.com/bluesky-social/atproto) repository. Some is developed in Go, and lives here.

<p align="center"><img src="https://static.bnewbold.net/tmp/under_construction_bar.gif" /></p>

Everything in this repository is an work in progress. Features and "Lexicons" may be removed or updated, software interfaces broken, etc.

We are developing in the open, but not ready to accept or review significant contributions. Keep checking back!

<p align="center"><img src="https://static.bnewbold.net/tmp/under_construction_bar.gif" /></p>


## What is atproto?

*not to be confused with the [AT command set](https://en.wikipedia.org/wiki/Hayes_command_set) or [Adenosine triphosphate](https://en.wikipedia.org/wiki/Adenosine_triphosphate)*

The Authenticated Transfer Protocol ("ATP" or "atproto") is a decentralized social media protocol, developed by [Bluesky PBC](https://blueskyweb.xyz). Learn more at:

- [Protocol Documentation](https://atproto.com/docs)
- [Overview Guide](https://atproto.com/guides/overview) 👈 Good place to start
- [Blogpost on self-authenticating data structures](https://blueskyweb.xyz/blog/3-6-2022-a-self-authenticating-social-protocol)


## Development

First, you will need the Go toolchain installed. We develop using the latest stable version of the language.

The Makefile provides wrapper commands for basic development:

    make build
    make test
    make fmt
    make lint

Individual commands can be run like:

    go run ./cmd/bigsky

## Are you a developer interested in building on atproto?

Bluesky is an open social network built on the AT Protocol, a flexible technology that will never lock developers out of the ecosystems that they help build. With atproto, third-party can be as seamless as first-party through custom feeds, federated services, clients, and more.

If you're a developer interested in building on atproto, we'd love to email you a Bluesky invite code. Simply share your GitHub (or similar) profile with us via [this form](https://forms.gle/BF21oxVNZiDjDhXF9).
