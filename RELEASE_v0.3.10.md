# agentruntime-mcp-go v0.3.10

## Outbound relay (MCP + harness workers)

Adds the `relay` package and wires it into `RunWithRouter` for self-hosted MCP connectors and external agent harness workers.

### Features

- **MCP relay** — WSS `/v1/connect`, sync JSON-RPC forward `POST /relay/mcp/instances/{id}` (request/response mux via `RequestMultiplexer`).
- **Harness relay** — WSS `/v1/harness/connect`, job dispatch `POST /relay/harness/workers/{id}/run`, shared pool picker `GET /relay/harness/pick`.
- **Heartbeat** — ping/pong sweeper; evict stale sessions after 90s.
- **Control callbacks** — `ControlCallback` notifies Control on MCP instance connect/disconnect (`canonical_url`, schema status).
- **Orchestrator callbacks** — `OrchestratorCallback` for harness worker-online and run-complete (agentruntime internal API).

### Env vars (router)

| Variable | Purpose |
|----------|---------|
| `RELAY_AUTH_TOKEN` | Optional bearer for WSS connect |
| `RELAY_PUBLIC_BASE_URL` | Canonical URL prefix for MCP instances |
| `HARNESS_ORCHESTRATOR_URL` | AgentRuntime base for harness callbacks |
| `MCP_CONTROL_SERVER_URL` | Control base for relay instance lifecycle |
| `MCP_CONTROL_INTERNAL_TOKEN` | Internal auth for callbacks |

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.10
```

**go-connectors router:** bump dependency, redeploy with `HARNESS_ORCHESTRATOR_URL` and `MCP_CONTROL_SERVER_URL` set per environment.

See monorepo `docs/architecture/HARNESS_WORKER_RELAY.md` and `docs/mcp/MCP_SELF_HOSTED_OUTBOUND_RELAY.md`.
