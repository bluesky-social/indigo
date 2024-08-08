
handlr: DNS TXT handle resolution wrapper
=========================================

This is a simple daemon which helps expose atproto handles to the public internet using the DNS TXT resolution mechanism, for services which don't want to individually configure DNS records.

The expected use-case is a network service which implements the `resolveHandle` atproto HTTP API method (aka, can resolve handles to DID against an internal datastore) for a large number of handles which are on nested sub-domains of a common suffix domain. In this case, it is difficult to use the HTTPS `/.well-known/` handle resolution mechanism, because SSL wildcard certs don't work with arbitrary nested domains. And DNS TXT resolution is difficult because it requires either running a DNS server, or integrating with an arbitrary provider API which may have restrictive limits.

This is a small DNS server which responds to TXT requests, and queries a backend service (over HTTP API) for actual resolution. Specifically, it calls the `/xrpc/com.atproto.identity.resolveHandle` GET endpoint with the query parameter `handle`, and expects a simple JSON object returned with a `did` field. An HTTP 400 or 404 error are both interpreted as an NXDOMAIN.


## Development Examples

Build the daemon from the top level of the indigo repo:

    go build ./cmd/handlr

Then run it configured to connect to the Bluesky appview:

    ./handlr --backend-host https://public.api.bsky.app serve

You can test real handles on the live network:

    dig @localhost -p 5333 TXT _atproto.atproto.com

A demo backend service is provided to experiment with local request. You need python3 and flask installed (`sudo apt install python3-flask`):

    # run the backend service in one terminal
    FLASK_APP=demo-backend python3 -m flask run --host 127.0.0.1 --port 5000

    # the daemon in another
    ./handlr serve

    # example resolution
    dig @localhost -p 5333 TXT _atproto.demo.handlr.example.com


## Deployment

To run this service for real, you'll need to create an `NS` DNS entry which covers the set of possible subdomains, such as `*.handlr.example.com`. You point this to `NS` record to the server running handlr, such as `srv.example.com`. It is assumed that the backend service is also running on this host.

NOTE: it might be possible to set up handlr to work with an entire base domain, like `*.example.com`, using glue records, but this hasn't been tried yet, and could break DNS for all other sub-domains (including A/AAAA and CNAME records).

NOTE: if you are on an Ubuntu server which is using `systemd-resolved` (bound to port 53), you need to disable it, for example [following these directions](https://www.turek.dev/posts/disable-systemd-resolved-cleanly/).

You can build the handlr binary (`go build ./cmd/handlr`) and then `scp` it to the server (if you are using the same operating system and architecture locally), and run it like:

    sudo LOG_LEVEL=warn ./handlr serve --bind :53 --backend-host http://localhost:5000 --domain-suffix handlr.example.com --ttl 300

Note that running with superuser privileges is required to bind to port 53 (DNS default). You'll need to ensure that inbound UDP is allowed for that port (might need to configure any firewall or iptables). Using a service manager like `systemd` is probably a good idea. Configuration can also be supplied via environment variables. You can adjust the log level, but probably want to keep it to `warn` for privacy and log volume reasons.

If all is working correctly, you should be able to resolve handles from any machine using regular DNS like:

    dig TXT _atproto.demo.handlr.example.com
