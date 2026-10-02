# agentruntime-mcp-go v0.3.19

## Security dependency refresh

Dependency-only release (no public API changes). Clears open Dependabot advisories on `go.mod`:

| Dependency | Was | Now |
|------------|-----|-----|
| `google.golang.org/grpc` | v1.75.0 | **v1.83.2** |
| `github.com/modelcontextprotocol/go-sdk` | v1.4.0 | **v1.4.1** |
| `go.opentelemetry.io/otel` (+ sdk, metric, trace) | v1.38–1.44 | **v1.45.0** |
| `go.opentelemetry.io/otel/exporters/otlp/...` | v1.38.0 | **v1.45.0** |
| `golang.org/x/net` | (via tidy) | **≥ v0.55.0** |

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.19
go mod tidy
```

From `connectors/go-connectors`, run the README `foreach` (or bulk script) so every adapter module pins **v0.3.19**.
