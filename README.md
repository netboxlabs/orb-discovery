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
discovery_1:
    config:
        schedule: "* * * 2 * *" #Cron expression
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
```

## REST API
The default `discovery` address is `localhost:8072`.  To change that you can specify host and port when starting `discovery`:
```sh
docker build --no-cache -t orb-discovery:develop -f orb-discovery/docker/Dockerfile .
docker -v / run orb-discovery:develop -c
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
> | `200`         | `application/json; charset=utf-8` | JSON data                                                           |

##### Example cURL

> ```javascript
>  curl -X GET -H "Content-Type: application/json" http://localhost:8072/api/v1/status
> ```

</details>

<details>
 <summary><code>GET</code> <code><b>/api/v1/capabilities</b></code> <code>(gets otelcol-contrib capabilities)</code></summary>

##### Parameters

> None

##### Responses

> | http code     | content-type                      | response                                                            |
> |---------------|-----------------------------------|---------------------------------------------------------------------|
> | `200`         | `application/json; charset=utf-8` | JSON data                                                           |

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
> | None      |  required | YAML object             | yaml format specified in [Policy RFC](#policy-rfc-v1)                 |
 

##### Responses

> | http code     | content-type                       | response                                                            |
> |---------------|------------------------------------|---------------------------------------------------------------------|
> | `201`         | `application/x-yaml; charset=UTF-8`| YAML object                                                         |
> | `400`         | `application/json; charset=UTF-8`  | `{ "detail": "invalid Content-Type. Only 'application/x-yaml' is supported" }`|
> | `400`         | `application/json; charset=UTF-8`  | Any policy error                                                    |
> | `400`         | `application/json; charset=UTF-8`  | `{ "detail": "only single policy allowed per request" }`           |
> | `403`         | `application/json; charset=UTF-8`  | `{ "detail": "config field is required" }`                         |
> | `409`         | `application/json; charset=UTF-8`  | `{ "detail": "policy already exists" }`                            |
 

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
> | `400`         | `application/json; charset=UTF-8` | Any policy deletion error                                           |
> | `404`         | `application/json; charset=UTF-8` | `{ "detail": "policy 'policy_name' not found" }`                    |

##### Example cURL

> ```javascript
>  curl -X DELETE http://localhost:8072/api/v1/policies/my_policy
> ```

</details>
