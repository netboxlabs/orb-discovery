# orb-discovery


### Config RFC
```yaml
discovery:
  config:
    target: grpc://localhost:8080/diode
    api_key: ${DIODE_API_KEY}
    host: 0.0.0.0
    port: 8072
```

### Policy RFC
```yaml
discovery:
  policies:
    discovery_1:
        config:
            schedule: "* * * * *" #Cron expression
            defaults:
                site: New York NY
        scope:
        - hostname: 192.168.0.32
          username: ${USER}
          password: admin
        - driver: eos
          hostname: 127.0.0.1
          username: admin
          password: ${ARISTA_PASSWORD}
          optional_args:
            enable_password: ${ARISTA_PASSWORD}
    discover_once: # will run only once
        scope:
        - hostname: 192.168.0.34
          username: ${USER}
          password: ${PASSWORD}
```
## Run orb-discovery
Orb-discovery can be run by installing it with pip
```sh
git clone https://github.com/netboxlabs/orb-discovery.git
cd orb-discovery/
pip install --no-cache-dir ./orb-discovery/
```

## Docker Image
Orb-discovery can be built and run using docker:
```sh
docker build --no-cache -t orb-discovery:develop -f orb-discovery/docker/Dockerfile .
docker run -v /local/orb:/usr/local/orb/ orb-discovery:develop -c /usr/local/orb/agent.yaml
```

## REST API
The default `discovery` address is `0.0.0.0:8072`.  To change that you can specify host and port on config file when starting `discovery`:
```sh
docker run -v /local/orb:/usr/local/orb/ orb-discovery:develop -c /usr/local/orb/agent.yaml
```

### Routes (v1)

#### Get runtime and capabilities information

<details>
 <summary><code>GET</code> <code><b>/api/v1/status</b></code> <code>(gets discovery runtime data)</code></summary>

##### Parameters

> None

##### Responses

> | http code     | content-type                      | response                                                            |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json; charset=utf-8` |  `{"version": "0.1.0","up_time_seconds": 3678 }`                    |

##### Example cURL

> ```javascript
>  curl -X GET -H "Content-Type: application/json" http://localhost:8072/api/v1/status
> ```

</details>

<details>
 <summary><code>GET</code> <code><b>/api/v1/capabilities</b></code> <code>(gets orb-discovery capabilities)</code></summary>

##### Parameters

> None

##### Responses

> | http code     | content-type                      | response                                                            |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json; charset=utf-8` | `{"supported_drivers":["ios","eos","junos","nxos","cumulus"]}`      |

##### Example cURL

> ```javascript
>  curl -X GET -H "Content-Type: application/json" http://localhost:8072/api/v1/capabilities
> ```

</details>

#### Policies Management


<details>
 <summary><code>POST</code> <code><b>/api/v1/policies</b></code> <code>(Creates a new policy)</code></summary>

##### Parameters

> | name      |  type     | data type               | description                                                           |
> |-----------|-----------|-------------------------|-----------------------------------------------------------------------|
> | None      |  required | YAML object             | yaml format specified in [Policy RFC](#policy-rfc)                    |
 

##### Responses

> | http code     | content-type                       | response                                                            |
> |---------------|------------------------------------|---------------------------------------------------------------------|
> | `201`         | `application/json; charset=UTF-8`  | `{"detail":"policy 'policy_name' was started"}`                     |
> | `400`         | `application/json; charset=UTF-8`  | `{ "detail": "invalid Content-Type. Only 'application/x-yaml' is supported" }`|
> | `400`         | `application/json; charset=UTF-8`  | Any other policy error                                              |
> | `403`         | `application/json; charset=UTF-8`  | `{ "detail": "config field is required" }`                          |
> | `409`         | `application/json; charset=UTF-8`  | `{ "detail": "policy 'policy_name' already exists" }`               |
 

##### Example cURL

> ```javascript
>  curl -X POST -H "Content-Type: application/x-yaml" --data @post.yaml http://localhost:8072/api/v1/policies
> ```

</details>

<details>
 <summary><code>DELETE</code> <code><b>/api/v1/policies/{policy_name}</b></code> <code>(delete a existing policy)</code></summary>

##### Parameters

> | name              |  type     | data type      | description                         |
> |-------------------|-----------|----------------|-------------------------------------|
> |   `policy_name`   |  required | string         | The unique policy name              |

##### Responses

> | http code     | content-type                      | response                                                            |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json; charset=UTF-8` | `{ "detail": "policy 'policy_name' was deleted" }`                  |
> | `400`         | `application/json; charset=UTF-8` | Any other policy deletion error                                     |
> | `404`         | `application/json; charset=UTF-8` | `{ "detail": "policy 'policy_name' not found" }`                    |

##### Example cURL

> ```javascript
>  curl -X DELETE http://localhost:8072/api/v1/policies/policy_name
> ```

</details>
