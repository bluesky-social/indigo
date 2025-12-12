# Deploying Tap to Railway

Railway makes it easy to deploy Tap with persistent storage in just a few minutes.

## 1. Create a New Project

1. Create a [Railway account](https://railway.app) if you don't have one
2. From your dashboard, click **New Project**
3. Select **Docker Image**
4. Paste in `ghcr.io/bluesky-social/indigo/tap:latest` and press Enter

## 2. Add Persistent Storage

1. Right-click on your new Tap service and select **Attach Volume**
2. Enter `/data` as the mount path

## 3. Configure Environment Variables

Click on your Tap service, then go to the **Variables** tab and add:

| Variable | Value | Purpose |
|----------|-------|---------|
| `TAP_LOG_LEVEL` | `error` | Reduces log noise (recommended for Railway's free tier) |
| `TAP_ADMIN_PASSWORD` | *(see below)* | Protects your API with Basic auth |

**Generating a secure admin password:**
```bash
openssl rand -hex 16
```

## 4. Apply Changes

Press **Shift + Enter** (or click Deploy) to apply your configuration.

## 5. Set Up Networking

1. Go to the **Settings** tab
2. Under **Networking**, click **Generate Domain**
3. Set the port to `2480`
4. Copy your new public URL

## 6. Test Your Deployment

```bash
curl -u admin:YOUR_PASSWORD https://YOUR_PROJECT.up.railway.app/health
```

You should see `{"status":"ok"}`.

## Next Steps

You're ready to start syncing! See the main [README](./README.md) for API usage.
