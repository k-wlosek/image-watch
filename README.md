# image-watch

[![Build Status](https://img.shields.io/github/actions/workflow/status/k-wlosek/image-watch/build.yml?style=flat-square&logo=github)](https://ghcr.io/k-wlosek/image-watch)
[![container](https://img.shields.io/github/v/tag/k-wlosek/image-watch?label=image&sort=semver)](https://ghcr.io/k-wlosek/image-watch)
[![codecov](https://codecov.io/github/k-wlosek/image-watch/graph/badge.svg?token=GIO95DOJB3)](https://codecov.io/github/k-wlosek/image-watch)

image-watch is a read-only daemon that monitors the containers running on a
host. When an image a container uses changes upstream, it notifies about the
tag and platform in use - reporting the kind of change (newer release, re-pushed
tag, another-platform-only update), once per event, filtered by policy.

image-watch does not pull, stop, or restart containers.

## Events

| Type                           | Fires when                                              | Example                                 |
| ------------------------------ | ------------------------------------------------------- | --------------------------------------- |
| `PATCH_AVAILABLE`              | a newer patch of the running SemVer tag exists          | `redis:7.2.0 → 7.2.4`                   |
| `MINOR_AVAILABLE`              | a newer minor release exists                            | `app:1.26.4 → 1.27.0`                   |
| `MAJOR_AVAILABLE`              | a newer major release exists                            | `app:1.5.0 → 2.0.0`                     |
| `APPLICATION_*_AVAILABLE`      | the application component of a composite tag advanced   | `1.2.3-alpine3.22 → 1.2.4-alpine3.22`   |
| `BASE_ADVANCEMENT_AVAILABLE`   | the base component of a composite tag advanced          | `1.2.3-alpine3.22 → 1.2.3-alpine3.23`   |
| `FAMILY_ADVANCEMENT_AVAILABLE` | an imprecise family tag moved to a new point release    | `postgres:16 → 16.4`                    |
| `TAG_CHANGED`                  | a mutable reference resolved to a different digest      | `latest (v1.18.1) → latest (v1.18.2)`   |
| `TAG_MUTATED`                  | a fixed-looking tag was re-pushed to a different digest | `redis:7.2 → 7.2`                       |
| `OTHER_PLATFORM_UPDATE`        | a newer release exists, but only for another platform   | `server:1.2.3 → 1.2.4 (linux/386 only)` |

In a composite tag the application, base, and combined candidates are evaluated 
separately; when both advance, the combined candidate (`1.2.4-alpine3.23`) 
is also reported. For `TAG_CHANGED`, image-watch resolves what release the moved
tag now serves (newest-first, bounded by `enrichment.max_tags`) and uses it as the candidate.

## Quick start

### Docker

```bash
mkdir -p data
# If you want to use the example config, fetch it:
curl -sfL https://raw.githubusercontent.com/k-wlosek/image-watch/main/deploy/docker/config.yaml.example > config.yaml
docker run -d --name image-watch \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v "$PWD/config.yaml:/etc/image-watch/config.yaml:ro" \
  -v "$PWD/data:/var/lib/image-watch" \
  -p 127.0.0.1:9090:9090 \
  ghcr.io/k-wlosek/image-watch:latest
```

Or check the compose file in [`deploy/docker/compose.yaml`](deploy/docker/compose.yaml) and run it with `docker compose up -d`.

### Podman

Similar as [Docker](#docker), but with the Podman socket mounted instead.

Setting `runtime.type` to `podman` will default to `unix:///run/podman/podman.sock` if no endpoint is given, but you can override it with `runtime.endpoint` in the config file or `IMAGE_WATCH_RUNTIME_ENDPOINT` environment variable.

Run the published image with the socket mounted in:

```bash
docker run -d --name image-watch \
  -v "$XDG_RUNTIME_DIR/podman/podman.sock:/run/podman/podman.sock:ro" \
  -e IMAGE_WATCH_RUNTIME_ENDPOINT=unix:///run/podman/podman.sock \
  -v "$PWD/config.yaml:/etc/image-watch/config.yaml:ro" \
  -v "$PWD/data:/var/lib/image-watch" \
  -p 127.0.0.1:9090:9090 \
  ghcr.io/k-wlosek/image-watch:latest
```

### Go

```bash
go install github.com/k-wlosek/image-watch/cmd/image-watch@latest
image-watch check
```

The one-shot `check` lists every running container, checks each unique image
against its registry, prints the raw detected events, then runs the actual
notification pipeline (policy-filtered, deduplicated, delivered to stdout by
default). Run it twice with nothing changed and the second run's notification
section is empty since they were already sent.

Run it continuously:

```bash
image-watch daemon
```

The daemon checks immediately, then once per `check_interval` (default `6h`). 
While running it serves `/healthz` and `/metrics`.

## Configuration

The defaults work against a local Docker socket and need no file at all. A
complete example is in [`deploy/docker/config.yaml.example`](deploy/docker/config.yaml.example).

To override the defaults, write a YAML file and either put it at
`/etc/image-watch/config.yaml` or point `IMAGE_WATCH_CONFIG_PATH` at it.

The file does not need to be complete - missing fields are assumed to be defaults.
If you only want to override a few fields, you can do that. Custom 
registry/authenticated registry users can use just the `registries` section,
those who want to be notified via ntfy or webhook can use just the 
`notify` section, and so on.

Some settings can be overridden without YAML at all and they take
precedence over it:

```
IMAGE_WATCH_CHECK_INTERVAL
IMAGE_WATCH_STATE_PATH
IMAGE_WATCH_RUNTIME_TYPE
IMAGE_WATCH_RUNTIME_ENDPOINT
IMAGE_WATCH_METRICS_LISTEN
```

### Notification targets

By default image-watch notifies to stdout. See the [example config](deploy/docker/config.yaml.example)
for ntfy/webhook configuration. The webhook target POSTs one
JSON payload per event:

```json
{
  "event": "PATCH_AVAILABLE",
  "image": "docker.io/library/redis",
  "current": "7.2.0",
  "candidate": "7.2.4",
  "platform": { "os": "linux", "architecture": "amd64" },
  "containers": ["redis-a", "redis-b"],
  "suppressed": ["redis-legacy"]
}
```

`containers` lists the running containers for which this update is relevant;
`suppressed` lists containers whose per-container labels disallow it (see
below). Both are omitted when empty.

### Private and plain-http registries

Per-host credentials, plain-HTTP (an explicit opt-in via `scheme: http`), and
private-CA bundles (`ca_file`, replacing the system trust store for that host)
are configured under `registries` - see the example config. Public registries
need no configuration at all.

### Per-container control via labels

| Label                                 | Effect                                                                              |
| ------------------------------------- | ----------------------------------------------------------------------------------- |
| image-watch.skip=true                 | container is ignored entirely (value checked as the string `"true"`)                |
| image-watch.policy.patch              | enable/disable `PATCH_AVAILABLE` / `APPLICATION_PATCH_AVAILABLE` for this container |
| image-watch.policy.minor              | `MINOR_AVAILABLE` / `APPLICATION_MINOR_AVAILABLE`                                   |
| image-watch.policy.major              | `MAJOR_AVAILABLE` / `APPLICATION_MAJOR_AVAILABLE`                                   |
| image-watch.policy.family-advancement | `FAMILY_ADVANCEMENT_AVAILABLE`                                                      |
| image-watch.policy.base-advancement   | `BASE_ADVANCEMENT_AVAILABLE`                                                        |
| image-watch.policy.tag-changed        | `TAG_CHANGED`                                                                       |
| image-watch.policy.tag-mutation       | `TAG_MUTATED`                                                                       |
| image-watch.policy.other-platform     | `OTHER_PLATFORM_UPDATE`                                                             |

Config and label policy merge permissively (an event is notified if any source
allows it). Labels apply per container: when containers on the same image have
different label policies, an event is delivered only for the containers that
allow it (the others appear in the notification's `suppressed` list), and an
event that no container allows is dropped entirely.

## Observability

- `GET /healthz` - returns `200 OK` if the daemon is running
- `GET /metrics` - Prometheus text metrics (see below)

<details>
<summary>image-watch specific metrics</summary>

```
# HELP image_watch_check_duration_seconds Duration of check cycles, in seconds.
# TYPE image_watch_check_duration_seconds histogram
# HELP image_watch_check_errors_total Total number of check cycles that failed entirely (e.g. the container runtime was unavailable).
# TYPE image_watch_check_errors_total counter
# HELP image_watch_checks_total Total number of check cycles performed.
# TYPE image_watch_checks_total counter
# HELP image_watch_containers Number of running containers observed in the most recent check.
# TYPE image_watch_containers gauge
# HELP image_watch_digest_drift Whether any running container for this image stream (repository, tag, platform) is on a digest that differs from what the registry currently serves (1) or matches it (0). Retains its last-known value during a registry outage rather than dropping to 0 -- see image_watch_observation_stale.
# TYPE image_watch_digest_drift gauge
# HELP image_watch_enrichment_attempts_total Total number of opportunistic latest-tag enrichment attempts.
# TYPE image_watch_enrichment_attempts_total counter
# HELP image_watch_enrichment_failures_total Total number of enrichment attempts that did not find a matching version within budget.
# TYPE image_watch_enrichment_failures_total counter
# HELP image_watch_enrichment_success_total Total number of enrichment attempts that found a matching version.
# TYPE image_watch_enrichment_success_total counter
# HELP image_watch_images Number of unique monitored images in the most recent check.
# TYPE image_watch_images gauge
# HELP image_watch_notification_errors_total Total number of notification delivery failures.
# TYPE image_watch_notification_errors_total counter
# HELP image_watch_notifications_total Total number of notifications delivered.
# TYPE image_watch_notifications_total counter
# HELP image_watch_observation_stale Whether the most recent check for this image stream (repository, tag, platform) failed (1) or succeeded (0).
# TYPE image_watch_observation_stale gauge
# HELP image_watch_registry_errors_total Total number of failed requests to each registry host.
# TYPE image_watch_registry_errors_total counter
# HELP image_watch_registry_request_duration_seconds Duration of requests to each registry host, in seconds.
# TYPE image_watch_registry_request_duration_seconds histogram
# HELP image_watch_registry_requests_total Total number of requests made to each registry host.
# TYPE image_watch_registry_requests_total counter
# HELP image_watch_updates_available Whether an update is currently known to be available (1) or not (0), per monitored image stream (repository, tag, platform) and event type. Retains its last-known value during a registry outage rather than dropping to 0 -- see image_watch_observation_stale.
# TYPE image_watch_updates_available gauge
```
</details>

<details>
<summary>standard process/Go metrics</summary>

```
# HELP go_gc_duration_seconds A summary of the wall-time pause (stop-the-world) duration in garbage collection cycles.
# TYPE go_gc_duration_seconds summary
# HELP go_gc_gogc_percent Heap size target percentage configured by the user, otherwise 100. This value is set by the GOGC environment variable, and the runtime/debug.SetGCPercent function. Sourced from /gc/gogc:percent.
# TYPE go_gc_gogc_percent gauge
# HELP go_gc_gomemlimit_bytes Go runtime memory limit configured by the user, otherwise math.MaxInt64. This value is set by the GOMEMLIMIT environment variable, and the runtime/debug.SetMemoryLimit function. Sourced from /gc/gomemlimit:bytes.
# TYPE go_gc_gomemlimit_bytes gauge
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
# HELP go_info Information about the Go environment.
# TYPE go_info gauge
# HELP go_memstats_alloc_bytes Number of bytes allocated in heap and currently in use. Equals to /memory/classes/heap/objects:bytes.
# TYPE go_memstats_alloc_bytes gauge
# HELP go_memstats_alloc_bytes_total Total number of bytes allocated in heap until now, even if released already. Equals to /gc/heap/allocs:bytes.
# TYPE go_memstats_alloc_bytes_total counter
# HELP go_memstats_buck_hash_sys_bytes Number of bytes used by the profiling bucket hash table. Equals to /memory/classes/profiling/buckets:bytes.
# TYPE go_memstats_buck_hash_sys_bytes gauge
# HELP go_memstats_frees_total Total number of heap objects frees. Equals to /gc/heap/frees:objects + /gc/heap/tiny/allocs:objects.
# TYPE go_memstats_frees_total counter
# HELP go_memstats_gc_sys_bytes Number of bytes used for garbage collection system metadata. Equals to /memory/classes/metadata/other:bytes.
# TYPE go_memstats_gc_sys_bytes gauge
# HELP go_memstats_heap_alloc_bytes Number of heap bytes allocated and currently in use, same as go_memstats_alloc_bytes. Equals to /memory/classes/heap/objects:bytes.
# TYPE go_memstats_heap_alloc_bytes gauge
# HELP go_memstats_heap_idle_bytes Number of heap bytes waiting to be used. Equals to /memory/classes/heap/released:bytes + /memory/classes/heap/free:bytes.
# TYPE go_memstats_heap_idle_bytes gauge
# HELP go_memstats_heap_inuse_bytes Number of heap bytes that are in use. Equals to /memory/classes/heap/objects:bytes + /memory/classes/heap/unused:bytes
# TYPE go_memstats_heap_inuse_bytes gauge
# HELP go_memstats_heap_objects Number of currently allocated objects. Equals to /gc/heap/objects:objects.
# TYPE go_memstats_heap_objects gauge
# HELP go_memstats_heap_released_bytes Number of heap bytes released to OS. Equals to /memory/classes/heap/released:bytes.
# TYPE go_memstats_heap_released_bytes gauge
# HELP go_memstats_heap_sys_bytes Number of heap bytes obtained from system. Equals to /memory/classes/heap/objects:bytes + /memory/classes/heap/unused:bytes + /memory/classes/heap/released:bytes + /memory/classes/heap/free:bytes.
# TYPE go_memstats_heap_sys_bytes gauge
# HELP go_memstats_last_gc_time_seconds Number of seconds since 1970 of last garbage collection.
# TYPE go_memstats_last_gc_time_seconds gauge
# HELP go_memstats_mallocs_total Total number of heap objects allocated, both live and gc-ed. Semantically a counter version for go_memstats_heap_objects gauge. Equals to /gc/heap/allocs:objects + /gc/heap/tiny/allocs:objects.
# TYPE go_memstats_mallocs_total counter
# HELP go_memstats_mcache_inuse_bytes Number of bytes in use by mcache structures. Equals to /memory/classes/metadata/mcache/inuse:bytes.
# TYPE go_memstats_mcache_inuse_bytes gauge
# HELP go_memstats_mcache_sys_bytes Number of bytes used for mcache structures obtained from system. Equals to /memory/classes/metadata/mcache/inuse:bytes + /memory/classes/metadata/mcache/free:bytes.
# TYPE go_memstats_mcache_sys_bytes gauge
# HELP go_memstats_mspan_inuse_bytes Number of bytes in use by mspan structures. Equals to /memory/classes/metadata/mspan/inuse:bytes.
# TYPE go_memstats_mspan_inuse_bytes gauge
# HELP go_memstats_mspan_sys_bytes Number of bytes used for mspan structures obtained from system. Equals to /memory/classes/metadata/mspan/inuse:bytes + /memory/classes/metadata/mspan/free:bytes.
# TYPE go_memstats_mspan_sys_bytes gauge
# HELP go_memstats_next_gc_bytes Number of heap bytes when next garbage collection will take place. Equals to /gc/heap/goal:bytes.
# TYPE go_memstats_next_gc_bytes gauge
# HELP go_memstats_other_sys_bytes Number of bytes used for other system allocations. Equals to /memory/classes/other:bytes.
# TYPE go_memstats_other_sys_bytes gauge
# HELP go_memstats_stack_inuse_bytes Number of bytes obtained from system for stack allocator in non-CGO environments. Equals to /memory/classes/heap/stacks:bytes.
# TYPE go_memstats_stack_inuse_bytes gauge
# HELP go_memstats_stack_sys_bytes Number of bytes obtained from system for stack allocator. Equals to /memory/classes/heap/stacks:bytes + /memory/classes/os-stacks:bytes.
# TYPE go_memstats_stack_sys_bytes gauge
# HELP go_memstats_sys_bytes Number of bytes obtained from system. Equals to /memory/classes/total:byte.
# TYPE go_memstats_sys_bytes gauge
# HELP go_sched_gomaxprocs_threads The current runtime.GOMAXPROCS setting, or the number of operating system threads that can execute user-level Go code simultaneously. Sourced from /sched/gomaxprocs:threads.
# TYPE go_sched_gomaxprocs_threads gauge
# HELP go_threads Number of OS threads created.
# TYPE go_threads gauge
# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
# TYPE process_cpu_seconds_total counter
# HELP process_max_fds Maximum number of open file descriptors.
# TYPE process_max_fds gauge
# HELP process_network_receive_bytes_total Number of bytes received by the process over the network.
# TYPE process_network_receive_bytes_total counter
# HELP process_network_transmit_bytes_total Number of bytes sent by the process over the network.
# TYPE process_network_transmit_bytes_total counter
# HELP process_open_fds Number of open file descriptors.
# TYPE process_open_fds gauge
# HELP process_resident_memory_bytes Resident memory size in bytes.
# TYPE process_resident_memory_bytes gauge
# HELP process_start_time_seconds Start time of the process since unix epoch in seconds.
# TYPE process_start_time_seconds gauge
# HELP process_virtual_memory_bytes Virtual memory size in bytes.
# TYPE process_virtual_memory_bytes gauge
# HELP process_virtual_memory_max_bytes Maximum amount of virtual memory available in bytes.
# TYPE process_virtual_memory_max_bytes gauge
```

</details>

## CLI

```
image-watch daemon        run continuously (default if no subcommand given)
image-watch check         one check-and-exit cycle
image-watch healthcheck   query the local /healthz; exit 0/1 (used by Docker HEALTHCHECK)
image-watch version       print version
```

## Development

```bash
make build
make test
make test-live    # live tests - against Docker Hub and a real Docker daemon
make image        # builds the docker image
./scripts/test-smoke.sh  # runs a full end-to-end check against a real Docker daemon

# coverage report
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Not implemented yet

- [ ] containerd runtime adapter
- [ ] CalVer and similar schemes (currently treated as plain SemVer, not as a calendrical scheme)
- [ ] custom grammars for non-SemVer schemes
- [ ] credential-helper integration for registries
- [ ] support for digest-pinned references (`image@sha256:…`) in the notification pipeline (they are recognized but excluded from analysis)
