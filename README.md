# etcdisc

`etcdisc` is a phase 1 CP-only registry, discovery, config, and basic A2A control plane built on etcd.

## Run in WSL

All development, testing, and local operations must run inside WSL.

```bash
make run
```

## Local stack

```bash
make up
```

This starts:

- `etcd` on `127.0.0.1:2379`
- HTTP server on `127.0.0.1:8080`
- gRPC server on `127.0.0.1:9090`

## Test

```bash
make test
```

To run real etcd integration tests after starting the local stack:

```bash
make integration
```
