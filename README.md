# docker-exporter

A simple Prometheus exporter for Docker container metrics (CPU, memory, network) with rich labels for node, service, and stack.

## Features

- Exposes per‑container metrics:
    - Memory usage, RSS, cache, limits, max usage.
    - CPU usage ratio (per scrape interval).
    - Network RX/TX bytes.
- Adds useful labels: `container_id`, `name`, `nodename`, `service`, `stack`.
- Caches metrics and scrapes Docker API on a configurable interval.
- Exposes its own health and internal metrics:
    - `/metrics` with standard `go_*`, `process_*` and custom exporter metrics.
    - `/healthz` HTTP 200 OK endpoint.
    - `docker_exporter_scrape_errors_total`.
    - `docker_exporter_last_scrape_success`.

***

## Environment variables

The exporter is configured via environment variables so you do not need to rebuild the image for small changes.

- `NODE_NAME`  
  Node name label to attach to all container metrics (usually the Swarm node hostname).

- `LISTEN_ADDR`  
  HTTP listen address for the exporter (default: `:9273`).

- `SCRAPE_INTERVAL_SECONDS`  
  Interval for polling the Docker API and updating cached metrics (default: `10` seconds).

- `DOCKER_HOST`  
  Optional. Standard Docker client variable. If set, the exporter will connect to this Docker endpoint instead of the default Unix socket.

***

## Exposed endpoints

- `GET /metrics`  
  Prometheus metrics endpoint.

- `GET /healthz`  
  Simple health check endpoint, returns HTTP 200 and `OK\n` body.

***

## Metrics

### Container metrics

All container metrics have the following labels:

- `container_id` – Docker container ID.
- `name` – container name (without leading `/`).
- `nodename` – node name, taken from `NODE_NAME`.
- `service` – service name, derived from Docker labels:
    - `com.docker.swarm.service.name` (Swarm service).
    - `com.docker.compose.service` (docker‑compose service fallback).
- `stack` – stack/compose project name, derived from:
    - `com.docker.stack.namespace` (Swarm stack).
    - `com.docker.compose.project` (docker‑compose project fallback).

Exported per‑container metrics:

- `docker_container_memory_usage_bytes{...}`  
  Current memory usage of the container.

- `docker_container_memory_rss_bytes{...}`  
  Resident set size (RSS) memory.

- `docker_container_memory_cache_bytes{...}`  
  Page cache memory.

- `docker_container_memory_limit_bytes{...}`  
  Configured memory limit for the container.

- `docker_container_memory_max_usage_bytes{...}`  
  Maximum observed memory usage since container start.

- `docker_container_cpu_usage_ratio{...}`  
  Approximate CPU usage ratio across all cores over the last scrape interval  
  (may be greater than 1 if the container uses multiple cores).

- `docker_container_network_receive_bytes_total{...}`  
  Total received bytes across all container interfaces.

- `docker_container_network_transmit_bytes_total{...}`  
  Total transmitted bytes across all container interfaces.

### Exporter internal metrics

- `docker_exporter_scrape_errors_total`  
  Counter of errors while scraping Docker API (listing containers or fetching stats).

- `docker_exporter_last_scrape_success`  
  Gauge with value:
    - `1` – last scrape cycle finished successfully for all containers.
    - `0` – last scrape cycle had at least one error.

Additionally you get the default Go runtime metrics:

- `go_*` (Go runtime metrics).
- `process_*` (process resource metrics).

***

## How it works

- The exporter runs a background loop (`collectLoop`) that:
    - Reads `SCRAPE_INTERVAL_SECONDS`.
    - On each tick:
        - Lists the running containers using the Docker SDK.
        - Uses a bounded worker pool to fetch stats for each container with `ContainerStatsOneShot`.
        - Parses `types.StatsJSON` and updates Prometheus gauges.
- Prometheus scrapes `/metrics` and receives the latest cached values without directly hammering the Docker API on each scrape.

***

## Running with docker run

Example:

```bash
docker run -d \
  --name docker-exporter \
  -e NODE_NAME="$(hostname)" \
  -e LISTEN_ADDR=":9273" \
  -e SCRAPE_INTERVAL_SECONDS="15" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 9273:9273 \
  renie613/docker-exporter:v0.1.0
```

Notes:

- `NODE_NAME` is set from the host hostname so you can join metrics from node‑exporter, cAdvisor, and docker‑exporter via the same label.
- `SCRAPE_INTERVAL_SECONDS` controls how often the exporter talks to the Docker daemon.

***

## Running as global service in Docker Swarm

Typical configuration as a global service on each Linux node:

```bash
docker service create \
  --name docker-exporter \
  --mode global \
  --network host \
  --publish mode=host,target=9273,published=9273 \
  --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
  --user root \
  --constraint 'node.platform.os == linux' \
  -e NODE_NAME='{{.Node.Hostname}}' \
  -e LISTEN_ADDR=':9273' \
  -e SCRAPE_INTERVAL_SECONDS='15' \
  renie613/docker-exporter:v0.1.0
```

Key points:

- `--mode global` runs one exporter per node, which is usually what you want.
- `NODE_NAME='{{.Node.Hostname}}'` lets each instance expose metrics for its node with a correct `nodename` label.
- `--network host` and `--publish mode=host` make the metrics available on the node’s IP at port `9273` (or your `LISTEN_ADDR`).

***

## Example Prometheus scrape config

If you generate targets from inventory (e.g. via Ansible), you can add docker‑exporter like this:

```yaml
- job_name: 'docker-exporter'
  file_sd_configs:
    - files:
        - /etc/prometheus/docker_exporter_targets.yml
```

Example `docker_exporter_targets.yml` entry:

```yaml
- targets: ['10.0.0.11:9273']
  labels:
    nodename: 'node-1'
```

Then you can query metrics such as:

```promql
docker_container_memory_usage_bytes{nodename="node-1"}
docker_container_cpu_usage_ratio{service="my-api", stack="prod"}
```

***

## Security considerations (docker.sock)

This exporter typically mounts `/var/run/docker.sock` inside the container to communicate with the Docker daemon.

Important notes:

- Access to `docker.sock` effectively grants root‑level control over the host Docker daemon.
- Run the exporter only on trusted hosts.
- Restrict access to the `/metrics` endpoint using network policies, firewalls, or reverse proxies.
- Consider placing the exporter on an internal network that is only reachable by your Prometheus server.

***

## Building from source

```bash
git clone https://github.com/your-org/docker_exporter.git
cd docker_exporter

go mod tidy
go build -o docker-exporter ./...
```

Build Docker image:

```bash
docker build -t renie613/docker-exporter:v0.1.0 .
```

***

## Versioning and image tags

This project uses semantic versioning for Docker image tags:

- Patch releases: `v0.1.1`, `v0.1.2` – bug fixes, no breaking changes.
- Minor releases: `v0.2.0`, `v0.3.0` – new metrics or labels added in a backward‑compatible way.
- Major releases: `v1.0.0` – breaking changes in metric names or label sets.

Typical tagging workflow:

```bash
docker build -t renie613/docker-exporter:v0.1.0 .
docker tag renie613/docker-exporter:v0.1.0 renie613/docker-exporter:latest

docker push renie613/docker-exporter:v0.1.0
docker push renie613/docker-exporter:latest
```

In production manifests (Compose, Swarm, Kubernetes) you should pin to a specific version:

```yaml
image: renie613/docker-exporter:v0.1.0
```

This avoids unexpected changes when new versions are released.