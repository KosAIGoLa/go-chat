# ck-chat

A Go IM project skeleton for an MVP-to-ten-million-level instant messaging system.

Architecture plan: [.tocodex/plans/go-im-ten-million-architecture.md](.tocodex/plans/go-im-ten-million-architecture.md)

## Skeleton scope

- Service entrypoints under `cmd/`.
- Shared bootstrapping under `internal/app`.
- Configuration model and sample config.
- Domain package placeholders for gateway, message, routing, sequence, group, receipt, enterprise and audit capabilities.
- Protobuf contract drafts under `api/proto`.
- SQL migration draft under `migrations`.
- Kubernetes/Helm placeholders under `deploy`.

## Quick start

```bash
go test ./...
go run ./cmd/gateway -config configs/local.yaml
```
