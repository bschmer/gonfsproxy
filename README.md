# gonfs_proxy

MVP NFS TCP proxy scaffold with per-procedure RPC fault injection.

This implementation is wire-transparent for backend NFS payloads:
- calls and replies are forwarded as ONC RPC records
- filehandles/cookies/attrs/verifiers are not rewritten
- injection is applied at RPC transport behavior only (delay/drop)

## Features

- Per-NFS-procedure RPC reply delay: `__rpc_delay__`
- Per-NFS-procedure delayed connection drop: `__rpc_drop__`
- Per-client policy lookup with `"default"` fallback
- Prometheus metrics:
  - `rpc_injection_delay_applied_total{procedure}`
  - `rpc_injection_delay_seconds_total{procedure,mode}`
  - `rpc_injection_delay_seconds{procedure,mode}` (histogram)
  - `rpc_injection_drop_applied_total{procedure}`
  - `rpc_call_forwarded_total{procedure}`
  - `rpc_backend_roundtrip_seconds{program,procedure}` (histogram)
  - `rpc_roundtrip_seconds{program,procedure}` (histogram, includes injected delay when reply is sent)
- Policy reload on `SIGHUP`

## Run

```bash
cd gonfs_proxy
cp policy.example.json policy.json
make build
./nfsproxy \
  -listen-nfs :20490 \
  -backend-nfs 127.0.0.1 \
  -backend-rpcbind-port 111 \
  -enable-mount \
  -listen-mount :8400 \
  -backend-mount 127.0.0.1 \
  -enable-rpcbind \
  -listen-rpcbind :111 \
  -rpcbind-host4 10.0.0.25 \
  -rpcbind-host6 2001:db8::25 \
  -metrics :9192 \
  -api :9193 \
  -verbose \
  -backend-secure-source-port \
  -backend-source-port-min 665 \
  -backend-source-port-max 1023 \
  -policy ./policy.json
```

## Build And Test

```bash
cd gonfs_proxy
make build      # builds ./nfsproxy
make test       # runs go test ./...
make ci         # fmt-check + vet + test + build
```

Then mount clients against proxy port `20490` instead of backend `2049`.
If using rpcbind-based discovery, point clients to `listen-rpcbind` port.
When local rpcbind is already running on `127.0.0.1:111` or `::1:111`, proxy registers NFS/mount mappings there and does not start embedded rpcbind.
`rpcbind-host4` and `rpcbind-host6` should be reachable by NFS clients. If omitted, proxy auto-selects non-loopback addresses when available.

`backend-nfs` and `backend-mount` accept either `host` or `host:port`.
- If port is omitted, proxy resolves port using backend rpcbind (`backend-rpcbind-port`, default `111`).
- `backend-mount` defaults to `backend-nfs` host, but can be set separately.
- When NFS and mount hosts differ and ports are omitted, rpcbind is queried on each respective host.

## Policy Format

Only `__rpc_delay__` and `__rpc_drop__` are interpreted in this scaffold.

```json
{
  "__rpc_delay__": {
    "READDIRPLUS": {
      "default": {
        "250": 1
      }
    }
  },
  "__rpc_drop__": {
    "READDIRPLUS": {
      "default": {
        "10000": 1
      }
    }
  }
}
```

Meaning:
- Delay all `READDIRPLUS` replies by 250ms.
- Delay all `READDIRPLUS` replies by 10s and then drop the client connection.
- If both apply, `__rpc_drop__` wins for mode and delay.

## Notes

- This is TCP NFSv3-focused and does not yet support UDP.
- It currently assumes one backend endpoint per proxied service.
- Packet mutation hooks (drop attrs, split/merge READDIRPLUS) are not implemented yet.
- By default backend-facing sockets use privileged source ports (665-1023). This usually requires root/CAP_NET_BIND_SERVICE.

## 💖 Support this project

If you find this project useful, consider supporting its development:

👉 https://github.com/sponsors/bschmer

Even a small contribution helps keep the project maintained and improved.

## Control API

Display current policy:

```bash
curl -s http://127.0.0.1:9193/api/v1/policy | jq
```

Replace full policy:

```bash
curl -sS -X PUT http://127.0.0.1:9193/api/v1/policy \
  -H 'Content-Type: application/json' \
  --data-binary @policy.json
```

Add/update one rule:

```bash
curl -sS -X POST http://127.0.0.1:9193/api/v1/rule \
  -H 'Content-Type: application/json' \
  -d '{"type":"__rpc_drop__","procedure":"READDIRPLUS","client":"default","weights":{"10000":1}}'
```

Delete one client rule for a procedure:

```bash
curl -sS -X DELETE "http://127.0.0.1:9193/api/v1/rule?type=__rpc_drop__&procedure=READDIRPLUS&client=default"
```

Delete all client rules for a procedure:

```bash
curl -sS -X DELETE "http://127.0.0.1:9193/api/v1/rule?type=__rpc_drop__&procedure=READDIRPLUS"
```


## Attribution
If you use this project, a mention like the following is appreciated:

"Uses bschmer's gonfsproxy"
