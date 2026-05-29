# Config Publish And Rollback

AI Model Gateway treats LLM routing config as an operational change, not as a
file edit that immediately mutates live traffic.

The control plane owns authoring config, revision history, publish records,
audit events, and rollback. The data plane only executes compiled snapshots
that `controld` has published to `gatewayd`.

## Why It Matters

Provider routing changes can affect every application behind the gateway. A
small config mistake can route traffic to the wrong provider, disable fallback,
break auth, change cost behavior, or remove a model that clients expect.

AI Model Gateway is designed so operators can answer these questions:

- What config is active right now?
- What would this draft config compile into?
- What changed between the active revision and the draft?
- Who published or rolled back a revision?
- Which revision can we return to during an incident?
- Did the data plane actually accept the snapshot?

## Runtime Model

```text
operator config.yaml
        |
        v
controld authoring config
        |
        v
compiled runtime snapshot
        |
        v
gatewayd ApplySnapshot
        |
        v
live data-plane routing
```

Important boundaries:

- `controld` is the only owner of config revision state.
- `gatewayd` does not read `config.yaml` directly.
- `gatewayd` executes compiled snapshots received from `controld`.
- `publisher-state.db` persists revision and publish state under the control
  runtime directory.
- Publish and rollback operations are recorded as audit events.

## Operator Workflow

1. Edit the authoring config.
2. Validate the file.
3. Preview the compiled runtime shape.
4. Diff the draft against the active revision or another revision.
5. Publish the intended revision through the Admin UI/API, or reload the
   authoring source from the CLI.
6. Watch gateway health, provider health, telemetry, and request logs.
7. Roll back to a known revision if the rollout is wrong.

## CLI Flow

Validate the YAML before using it:

```bash
./dist/gateway-cli validate configs/config.yaml
```

Inspect the active config:

```bash
./dist/gateway-cli config show
```

Preview a draft config:

```bash
./dist/gateway-cli config preview configs/config.yaml
```

Diff a draft config:

```bash
./dist/gateway-cli config diff --file configs/config.yaml
```

Diff two stored revisions:

```bash
./dist/gateway-cli config diff --from rev-001 --to rev-002
```

List recent publish history:

```bash
./dist/gateway-cli publish history
```

Reload the configured authoring source and publish the resulting revision:

```bash
./dist/gateway-cli reload
```

Roll back to a previous revision:

```bash
./dist/gateway-cli publish rollback rev-001
```

The CLI defaults to `http://127.0.0.1:18081` for the control plane. Use
`-server` and `-token` when connecting to another host or an authenticated
admin surface.

## Admin API Surface

The Admin UI and `gateway-cli` use the same control-plane API surface:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/admin/config` | Read active config metadata and policy |
| `GET` | `/api/admin/config/history` | Read revision history |
| `POST` | `/api/admin/config/validate` | Validate a draft config payload |
| `POST` | `/api/admin/config/preview` | Compile and summarize a draft config |
| `POST` | `/api/admin/config/diff` | Compare revisions or a draft file |
| `POST` | `/api/admin/config/update` | Store an updated config revision |
| `POST` | `/api/admin/config/reload` | Reload the authoring source and publish it |
| `POST` | `/api/admin/config/publish` | Publish a revision to `gatewayd` |
| `POST` | `/api/admin/config/rollback` | Publish an older revision as rollback |
| `GET` | `/api/admin/audit` | Review publish, rollback, validate, and update events |
| `GET` | `/api/admin/runtime/status` | Check whether data/control/telemetry are healthy |

Write operations require admin authorization and same-origin protection in the
browser path. Viewer tokens are intended for read-only inspection.

## What Publish Does

When a revision is published:

1. `controld` finds the requested revision.
2. The revision config is compiled into a gateway snapshot.
3. A publish record is staged.
4. `controld` calls `gatewayd` with `ApplySnapshot`.
5. If `gatewayd` accepts the snapshot, the publish record is marked observed
   and the revision becomes active.
6. If compilation or `ApplySnapshot` fails, the publish record is marked failed
   and the active revision is not advanced.

The publish record includes a publish ID, revision ID, snapshot ID, timestamp,
kind (`publish` or `rollback`), status, and error details when available.

## What Rollback Does

Rollback publishes an older revision with the publish kind set to `rollback`.
It uses the same compile, snapshot, `ApplySnapshot`, audit, and persistence path
as a normal publish.

This is deliberate: rollback should be observable and auditable. It should not
silently mutate files or replace only part of the runtime.

## Operational Checks

Before publish:

```bash
./dist/gateway-cli validate configs/config.yaml
./dist/gateway-cli config preview configs/config.yaml
./dist/gateway-cli config diff --file configs/config.yaml
./dist/gateway-cli provider list
./dist/gateway-cli provider test openai
```

Publish through the Admin UI/API, or reload the configured authoring source:

```bash
./dist/gateway-cli reload
```

After publish:

```bash
./dist/aigw status \
  -control-url http://127.0.0.1:18081 \
  -gateway-url http://127.0.0.1:18080 \
  -token "$ADMIN_TOKEN"

./dist/gateway-cli runtime status
./dist/gateway-cli publish history
./dist/gateway-cli audit 50
```

During incidents:

```bash
./dist/gateway-cli publish rollback <known-good-revision>
./dist/aigw logs -runtime-root .gateway-runtime gatewayd controld
```

## Persistence Notes

The control plane stores publisher state in:

```text
.gateway-runtime/control/publisher-state.db
```

or the equivalent control data directory from your deployment. When this file
exists, `controld` restores the persisted revision/history state at startup
instead of creating a new revision just because `config.yaml` changed.

That behavior protects production history. If you intentionally want to reseed
from YAML, stop the service, back up the control runtime directory, remove
`publisher-state.db`, and restart. Treat that as a reset operation, not as a
normal config rollout.

## How This Differs From Editing A Live Proxy File

| Live file edit | AI Model Gateway publish flow |
| --- | --- |
| File write and runtime effect can be coupled | Draft, preview, diff, publish, and rollback are separate steps |
| Data plane may read config directly | Data plane receives compiled snapshots from the control plane |
| Rollback often means manually restoring a file | Rollback publishes a known previous revision |
| Change evidence depends on external process | Publish, rollback, and validation events are auditable |
| Runtime acceptance may be implicit | `ApplySnapshot` success/failure is part of the publish result |

## Related Docs

- [Installation guide](installation.md)
- [Deployment guide](deployment.md)
- [CLI guide](cli.md)
- [Troubleshooting guide](troubleshooting.md)
- [Use cases](use-cases.md)
