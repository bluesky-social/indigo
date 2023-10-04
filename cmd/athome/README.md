
athome: Public Bluesky Web Home
===============================

```text
me: can we have public web interface?
mom: we have public web interface at home
public web interface at home:
```

1. run this web service somewhere
2. point one or more handle domains to it (CNAME or reverse proxy)
3. serves up profile and feed for that account only
4. fetches data from public bsky app view API


## Running athome

The recommended way to run `athome` is behind a `caddy` HTTPS server which does automatic on-demand SSL certificate registration (using Let's Encrypt).

Build and run `athome`:

    go build ./cmd/athome

    # will listen on :8200 by default
    ./athome serve

Create a `Caddyfile`:

```
{
  on_demand_tls {
    interval 1h
    burst 8
  }
}

:443 {
  reverse_proxy localhost:8200
  tls YOUREMAIL@example.com {
    on_demand
  }
}
```

Run `caddy`:

    caddy run


## Configuring a Handle

The easiest way, if there is no existing web service on the handle domain, is to get the handle resolution working with the DNS TXT record option, then point the domain itself to a `athome` service using an A/AAAA or CNAME record.

If there is an existing web service (eg, a blog), then handle resolution can be set up using either the DNS TXT mechanism or HTTP `/.well-known/` mechanism. Then HTTP proxy paths starting `/bsky` to an `athome` service.

Here is an nginx config snippet demonstrating HTTP proxying:

```
location /bsky {
    // in theory https:// should work, on default port?
    proxy_pass      http://athome.example.com:8200;
    proxy_set_header    X-Real-IP $remote_addr;
    proxy_set_header    Host      $http_host;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```
