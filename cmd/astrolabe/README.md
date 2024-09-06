
astrolabe: basic atproto network data explorer
==============================================

⚠️ This is a fun little proof-of-concept ⚠️


## Run It

The recommended way to run `astrolabe` is behind a `caddy` HTTPS server which does automatic on-demand SSL certificate registration (using Let's Encrypt).

Build and run `astrolabe`:

    go build ./cmd/astrolabe

    # will listen on :8400 by default
    ./astrolabe serve

Create a `Caddyfile`:

```
{
  on_demand_tls {
    interval 1h
    burst 8
  }
}

:443 {
  reverse_proxy localhost:8400
  tls YOUREMAIL@example.com {
    on_demand
  }
}
```

Run `caddy`:

    caddy run
