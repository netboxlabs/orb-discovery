# gnmi-discovery

Orb gNMI discovery backend — an event-driven network discovery service that maintains long-lived gNMI subscriptions and ingests device, interface, and hardware-inventory changes into NetBox via Diode within seconds of them occurring.

Unlike SNMP/NAPALM-based polling, `gnmi-discovery` reacts to ON_CHANGE notifications from the device itself (with SAMPLE and GET fallback for devices that don't support streaming), so NetBox stays up to date continuously rather than on a polling interval.

## How it works

1. For each target in a policy, `gnmi-discovery` opens a gNMI session and negotiates the best delivery mode (auto ladder: ON_CHANGE → SAMPLE → GET).
2. Inbound notifications are reconciled into a per-target in-memory model of device, interfaces, and hardware components.
3. After a configurable debounce window, the snapshot is translated to Diode entities (Device, Interface, Module/ModuleBay) and ingested.
4. The model is pruned each cycle so departed interfaces stop being ingested — removals are counted but not propagated as NetBox deletes (Diode delete unavailable).

## Requirements

A running [NetBox Diode](https://github.com/netboxlabs/diode) endpoint, or use `--dry-run` for local testing.

## Usage

```sh
Usage of gnmi-discovery:
  -diode-app-name-prefix string
        diode producer_app_name prefix
  -diode-client-id string
        diode client ID. Environment variable can be used by wrapping it in ${} (e.g. ${DIODE_CLIENT_ID})
  -diode-client-secret string
        diode client secret. Environment variable can be used by wrapping it in ${} (e.g. ${DIODE_CLIENT_SECRET})
  -diode-target string
        diode target. Environment variable can be used by wrapping it in ${} (e.g. ${DIODE_TARGET})
  -dry-run
        run in dry-run mode, do not ingest data
  -dry-run-output-dir string
        output dir for dry-run mode. Environment variable can be used by wrapping it in ${} (e.g. ${DRY_RUN_OUTPUT_DIR})
  -help
        show this help
  -host string
        server host (default "0.0.0.0")
  -log-format string
        log format (default "TEXT")
  -log-level string
        log level (default "INFO")
  -otel-endpoint string
        OpenTelemetry exporter endpoint (e.g. localhost:4317). Environment variable can be used by wrapping it in ${} (e.g. ${OTEL_ENDPOINT})
  -otel-export-period int
        Period in seconds between OpenTelemetry exports (default 10)
  -port int
        server port (default 8074)
  -profiles-dir string
        directory of gNMI profile overrides (empty = embedded profiles only)
```

## Policy RFC

```yaml
policies:
  gnmi_fabric:
    config:
      mode: auto           # auto (default) | on_change | sample | get
      debounce_ms: 2000    # flush delay after last notification (default 2000)
      sample_interval_ms: 300000   # SAMPLE subscription interval (default 300000 = 5m)
      get_interval_ms: 900000      # GET poll interval (default 900000 = 15m)
      defaults:
        site: New York NY          # NetBox site (default "undefined")
        role: Router               # NetBox device role (default "undefined")
        location: ""               # NetBox location (optional)
        tags: []                   # NetBox tags applied to all entities
        device:
          manufacturer: ""         # override manufacturer (optional)
          model: ""                # override model (optional)
          platform: ""             # override platform (optional)
          comments: ""
          tags: []
        interface:
          if_type: other           # default interface type (default "other")
          description: ""
          tags: []
    scope:
      targets:
        - host: 10.0.0.11:6030       # Arista EOS default gNMI port
          username: ${GNMI_USER}     # ${ENV_VAR} syntax supported
          password: ${GNMI_PASS}
          tls:
            skip_verify: true
            ca: /run/secrets/ca.pem  # optional mTLS
            cert: /run/secrets/cert.pem
            key: /run/secrets/key.pem
          profile: arista_eos        # pin a profile (auto-detect if omitted)
          mode: on_change            # per-target mode override
          netbox_id: 42              # pin to an existing NetBox device ID
          override_defaults:         # per-target defaults override
            site: Chicago IL

        - host: 10.0.0.21:57400     # Nokia SR-OS default gNMI port
          username: admin
          password: pw
```

### Delivery modes

| Mode | Behaviour |
|------|-----------|
| `auto` | Try ON_CHANGE first; fall back to SAMPLE, then GET on rejection |
| `on_change` | Subscribe STREAM ON_CHANGE; reconnect with debounce |
| `sample` | Subscribe STREAM SAMPLE at `sample_interval_ms` |
| `get` | Poll via gNMI Get every `get_interval_ms` |

### Bundled profiles

Profiles live in `mapping/gnmi-profiles/` and are compiled into the binary. Bundled profiles:

| Profile | Match |
|---------|-------|
| `_base` | fallback for any vendor |
| `arista_eos` | vendor = `Arista` |
| `nokia_sros` | vendor = `Nokia` |

To add vendor-specific overrides without rebuilding, mount a directory and pass `-profiles-dir`:

```sh
gnmi-discovery -profiles-dir /etc/gnmi-profiles ...
```

A profile override file uses `extends: _base` to inherit bundled paths and override only the differences.

## Run gnmi-discovery

```sh
git clone https://github.com/netboxlabs/orb-discovery.git
cd orb-discovery/gnmi-discovery/
make build
build/gnmi-discovery \
  --diode-target grpc://192.168.31.114:8080/diode \
  --diode-client-id '${DIODE_CLIENT_ID}' \
  --diode-client-secret '${DIODE_CLIENT_SECRET}'
```

Dry-run (no Diode required):

```sh
build/gnmi-discovery --dry-run --dry-run-output-dir /tmp/gnmi-dry --port 8074
```

## Docker Image

```sh
cd orb-discovery/gnmi-discovery/
docker build --no-cache -t gnmi-discovery:develop -f docker/Dockerfile .
docker run --net=host \
  -e GNMI_USER=admin \
  -e GNMI_PASS=secret \
  -e DIODE_CLIENT_ID=${DIODE_CLIENT_ID} \
  -e DIODE_CLIENT_SECRET=${DIODE_CLIENT_SECRET} \
  gnmi-discovery:develop \
  --diode-target grpc://192.168.31.114:8080/diode \
  --diode-client-id '${DIODE_CLIENT_ID}' \
  --diode-client-secret '${DIODE_CLIENT_SECRET}'
```

To use profile overrides at runtime:

```sh
docker run --net=host \
  -v /etc/my-gnmi-profiles:/etc/gnmi-profiles:ro \
  gnmi-discovery:develop \
  --diode-target grpc://... \
  --profiles-dir /etc/gnmi-profiles \
  ...
```

## Routes (v1)

### Get runtime and capabilities information

<details>
 <summary><code>GET</code> <code><b>/api/v1/status</b></code> <code>(gets gnmi-discovery runtime data)</code></summary>

##### Parameters

> None

##### Responses

> | http code | content-type | response |
> |-----------|--------------|----------|
> | `200` | `application/json; charset=utf-8` | `{"start_time":"...","up_time_seconds":3,"version":"0.0.0","policies":[{"name":"demo","status":"running","targets":[...],"runs":[...]}]}` |

##### Example cURL

> ```sh
>  curl -X GET -H "Content-Type: application/json" http://localhost:8074/api/v1/status
> ```

</details>

<details>
 <summary><code>GET</code> <code><b>/api/v1/capabilities</b></code> <code>(gets gnmi-discovery capabilities)</code></summary>

##### Parameters

> None

##### Responses

> | http code | content-type | response |
> |-----------|--------------|----------|
> | `200` | `application/json; charset=utf-8` | `{"capabilities":["targets","on_change","sample","get"]}` |

##### Example cURL

> ```sh
>  curl -X GET -H "Content-Type: application/json" http://localhost:8074/api/v1/capabilities
> ```

</details>

### Policies Management

<details>
 <summary><code>POST</code> <code><b>/api/v1/policies</b></code> <code>(Creates a new policy)</code></summary>

##### Parameters

> | name | type | data type | description |
> |------|------|-----------|-------------|
> | None | required | YAML object | yaml format specified in [Policy RFC](#policy-rfc) |

##### Responses

> | http code | content-type | response |
> |-----------|--------------|----------|
> | `201` | `application/json; charset=UTF-8` | `{"detail":"policies [policy_name] were started"}` |
> | `400` | `application/json; charset=UTF-8` | `{"detail":"invalid Content-Type. Only 'application/x-yaml' is supported"}` |
> | `400` | `application/json; charset=UTF-8` | Any other policy error |
> | `409` | `application/json; charset=UTF-8` | `{"detail":"policy 'policy_name' already exists"}` |

##### Example cURL

> ```sh
>  curl -X POST -H "Content-Type: application/x-yaml" --data-binary @policy.yaml http://localhost:8074/api/v1/policies
> ```

</details>

<details>
 <summary><code>DELETE</code> <code><b>/api/v1/policies/{policy_name}</b></code> <code>(delete an existing policy)</code></summary>

##### Parameters

> | name | type | data type | description |
> |------|------|-----------|-------------|
> | `policy_name` | required | string | The unique policy name |

##### Responses

> | http code | content-type | response |
> |-----------|--------------|----------|
> | `200` | `application/json; charset=UTF-8` | `{"detail":"policy 'policy_name' was deleted"}` |
> | `400` | `application/json; charset=UTF-8` | Any other policy deletion error |
> | `404` | `application/json; charset=UTF-8` | `{"detail":"policy 'policy_name' not found"}` |

##### Example cURL

> ```sh
>  curl -X DELETE http://localhost:8074/api/v1/policies/policy_name
> ```

</details>
