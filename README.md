# Vera Telephone

**The lightweight sidecar component of Vera-Stack**

---

## Overview

Telephone is a lightweight sidecar process that runs alongside your application and maintains a persistent WebSocket tunnel to the Plugboard reverse proxy. It enables dynamic routing by forwarding proxied requests from Plugboard to your local application.

### Key Responsibilities

* Establish and maintain WebSocket connection to Plugboard
* Register application mount points on startup
* Forward proxied HTTP requests to local backend (e.g., `localhost:3000`)
* Stream responses back through the WebSocket tunnel
* Handle connection failures and automatic reconnection
* Manage JWT token authentication and refresh

---

## How It Works

```
┌────────────────────────────────┐
│    Your Application Container  │
│                                │
│  ┌──────────────────────────┐  │
│  │   Telephone Sidecar      │  │
│  │                          │  │
│  │   1. Connect to          │  │
│  │      Plugboard (WS)      │  │
│  │   2. Register /api       │  │
│  │   3. Wait for requests   │  │
│  └──────────┬───────────────┘  │
│             │                  │
│             │ Forward to       │
│             │ localhost:3000   │
│             ▼                  │
│  ┌──────────────────────────┐  │
│  │   Your Application       │  │
│  │   (Express, Rails, etc.) │  │
│  │   Listening on :3000     │  │
│  └──────────────────────────┘  │
└────────────────────────────────┘
```

---

## Status

**Under Development** - Telephone is currently in the planning phase. Implementation has not yet started.

### Planned Features

* WebSocket client for persistent connection to Plugboard
* HTTP request forwarding to localhost backend
* Automatic reconnection with exponential backoff
* JWT token authentication
* Health check endpoint
* Configurable via CLI arguments or environment variables
* Docker sidecar deployment pattern
* Kubernetes sidecar integration

### Planned Deployment Patterns

* Standalone binary
* Docker sidecar container
* Kubernetes sidecar container
* Systemd service

---

## Conceptual Usage

Once implemented, Telephone will work something like:

```bash
# Planned CLI usage
telephone \
  --plugboard ws://plugboard:4000 \
  --mount /api \
  --port 3000 \
  --token <jwt-token>
```

Or as a Docker sidecar:

```yaml
# Planned Docker Compose pattern
services:
  app:
    image: my-app:latest
    ports:
      - "3000:3000"
  
  telephone:
    image: verastack/telephone:latest
    environment:
      PLUGBOARD_URL: ws://plugboard:4000
      MOUNT_PATH: /api
      BACKEND_PORT: 3000
      TELEPHONE_TOKEN: ${TELEPHONE_TOKEN}
```

---

## License

Licensed under the **MIT License**. See [../LICENSE](../LICENSE) for details.
