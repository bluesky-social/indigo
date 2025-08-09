
OAuth SDK Web App Example
=========================

This is a minimal Go web app showing how to use the OAuth client SDK.

To get started, generated a `.env` file with the following variables:

- `SESSION_SECRET` (required) is a random string for secure cookies, you can generate one with `openssl rand -hex 16`
- `CLIENT_HOSTNAME` (optional) is a public web hostname at which the running web app can be reached on the public web, with `https://`. It needs to actually be reachable by remote servers, not just your local web browser; you can use a service like `ngrok` if experimenting on a laptop. Or, if you leave this blank, the app will run as a "localhost dev app".
- `CLIENT_SECRET_KEY` (optional) is used to run as a "confidential" client, with client attestation. You can generate a private key with the `goat` CLI tool (`goat key generate -t P-256`)

And example file might look like:

```
SESSION_SECRET=49922828917dc6ac2f2fd2cca78735c3
CLIENT_SECRET_KEY=z42twLj2gZeJSeRgZ4yPyEb6Yg6nawhU2W8y2ETDDFFyvwym
CLIENT_HOSTNAME=a9a7c2e14c.ngrok-free.app
```

Then run the demo (`go run .`) and connect with a web browser.
