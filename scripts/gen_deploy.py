"""Generates the deploy/ tree (observability stack, dashboards, alerts, compose)."""
import json
import os


def w(p, s):
    os.makedirs(os.path.dirname(p) or ".", exist_ok=True)
    with open(p, "w", newline="\n") as f:
        f.write(s)


w("deploy/obs/docker-compose.yml", """services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.111.0
    command: ["--config=/etc/otelcol/config.yaml"]
    volumes:
      - ./otel-collector.yaml:/etc/otelcol/config.yaml:ro
    ports: ["4318:4318"]
    depends_on: [prometheus, tempo, loki]
  prometheus:
    image: prom/prometheus:v2.55.0
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --web.enable-otlp-receiver
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports: ["9090:9090"]
  tempo:
    image: grafana/tempo:2.6.1
    command: ["-config.file=/etc/tempo.yaml"]
    volumes:
      - ./tempo.yaml:/etc/tempo.yaml:ro
    ports: ["3200:3200"]
  loki:
    image: grafana/loki:3.2.0
    ports: ["3100:3100"]
  grafana:
    image: grafana/grafana:11.3.0
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ../grafana/dashboards:/var/lib/grafana/dashboards:ro
      - ../grafana/alerts:/etc/grafana/provisioning/alerting:ro
    ports: ["3000:3000"]
    depends_on: [prometheus, tempo, loki]
""")

w("deploy/obs/otel-collector.yaml", """receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 400
  batch: {}
exporters:
  otlphttp/prometheus:
    endpoint: http://prometheus:9090/api/v1/otlp
    tls:
      insecure: true
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlphttp/prometheus]
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp/tempo]
""")

w("deploy/obs/prometheus.yml", """global:
  scrape_interval: 15s
otlp:
  promote_resource_attributes:
    - service.name
    - service.instance.id
""")

w("deploy/obs/tempo.yaml", """server:
  http_listen_port: 3200
distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
storage:
  trace:
    backend: local
    wal:
      path: /tmp/tempo/wal
    local:
      path: /tmp/tempo/blocks
""")

w("deploy/obs/grafana/provisioning/datasources/datasources.yaml", """apiVersion: 1
datasources:
  - name: Prometheus
    uid: prometheus
    type: prometheus
    url: http://prometheus:9090
    isDefault: true
  - name: Tempo
    uid: tempo
    type: tempo
    url: http://tempo:3200
  - name: Loki
    uid: loki
    type: loki
    url: http://loki:3100
""")

w("deploy/obs/grafana/provisioning/dashboards/dashboards.yaml", """apiVersion: 1
providers:
  - name: seedlog
    type: file
    options:
      path: /var/lib/grafana/dashboards
""")


def panel(i, title, expr, unit, x, y):
    return {
        "id": i, "type": "timeseries", "title": title,
        "datasource": {"type": "prometheus", "uid": "prometheus"},
        "gridPos": {"h": 8, "w": 12, "x": x, "y": y},
        "fieldConfig": {"defaults": {"unit": unit}, "overrides": []},
        "targets": [{"refId": "A", "expr": expr}],
    }


def dash(uid, title, panels):
    ps = [panel(i + 1, t, e, u, (i % 2) * 12, (i // 2) * 8) for i, (t, e, u) in enumerate(panels)]
    return json.dumps({"uid": uid, "title": title, "schemaVersion": 39, "version": 1,
                       "refresh": "10s", "panels": ps}, indent=2)


w("deploy/grafana/dashboards/explorer.json", dash("seedlog-explorer", "SeedLog Explorer", [
    ("Seeds per second", "sum(rate(seedlog_sim_seeds_total[1m]))", "ops"),
    ("Events per second per core",
     "sum(rate(seedlog_sim_events_total[1m])) / sum(rate(seedlog_sim_worker_busy_seconds_total[1m]))", "ops"),
    ("Simulated seconds per busy core-second",
     "sum(rate(seedlog_sim_virtual_time_seconds_total[1m])) / sum(rate(seedlog_sim_worker_busy_seconds_total[1m]))", "short"),
    ("Parallel efficiency",
     "sum(rate(seedlog_sim_worker_busy_seconds_total[1m])) / max(seedlog_sim_workers)", "percentunit"),
    ("Outcome breakdown", "sum by (outcome) (rate(seedlog_sim_seeds_total[1m]))", "ops"),
    ("Fault mix", "sum by (kind) (rate(seedlog_sim_faults_total[1m]))", "ops"),
    ("Coverage hits by point", "sum by (point) (seedlog_sim_coverage_hits_total)", "short"),
    ("GC pause p99", "histogram_quantile(0.99, sum by (le) (rate(go_gc_duration_seconds_bucket[1m])))", "s"),
    ("Allocation rate", "rate(go_memstats_alloc_bytes_total[1m])", "Bps"),
]))

w("deploy/grafana/dashboards/cluster.json", dash("seedlog-cluster", "SeedLog Cluster", [
    ("Term per node", "seedlog_raft_term", "short"),
    ("Role per node", "seedlog_raft_role", "short"),
    ("Leader changes", "rate(seedlog_raft_leader_changes_total[1m])", "ops"),
    ("Commit minus applied", "seedlog_raft_commit_index - seedlog_raft_applied_index", "short"),
    ("Request rate", "sum by (op) (rate(seedlog_kv_requests_total[1m]))", "ops"),
    ("Error ratio",
     'sum(rate(seedlog_kv_requests_total{outcome!="ok"}[1m])) / sum(rate(seedlog_kv_requests_total[1m]))', "percentunit"),
    ("Request latency p99",
     "histogram_quantile(0.99, sum by (le, op) (rate(seedlog_kv_request_duration_seconds_bucket[1m])))", "s"),
    ("fsync p99",
     "histogram_quantile(0.99, sum by (le) (rate(seedlog_wal_fsync_duration_seconds_bucket[1m])))", "s"),
    ("Election duration p99",
     "histogram_quantile(0.99, sum by (le) (rate(seedlog_raft_election_duration_seconds_bucket[5m])))", "s"),
    ("WAL recovery truncated bytes", "rate(seedlog_wal_recovery_truncated_bytes_total[5m])", "short"),
]))

w("deploy/grafana/dashboards/study.json", dash("seedlog-study", "SeedLog Study", [
    ("Median time to detect by bug and method",
     "histogram_quantile(0.5, sum by (le, bug, method) (rate(seedlog_study_time_to_detect_seconds_bucket[1h])))", "s"),
    ("Detection rate",
     'sum by (bug, method) (seedlog_study_trials_total{outcome="found"}) / sum by (bug, method) (seedlog_study_trials_total)',
     "percentunit"),
    ("Shrink ratio",
     'sum(rate(seedlog_shrink_events_sum{stage="after"}[1h])) / sum(rate(seedlog_shrink_events_sum{stage="before"}[1h]))',
     "percentunit"),
    ("Trials", "sum by (method) (seedlog_study_trials_total)", "short"),
]))

w("deploy/grafana/dashboards/triage.json", dash("seedlog-triage", "SeedLog Triage", [
    ("Violations by detector", "sum by (detector) (increase(seedlog_sim_violations_total[1h]))", "short"),
    ("Checker verdicts", "sum by (detector) (increase(seedlog_check_violations_total[1h]))", "short"),
]))

alerts = [
    ("explorer_violation", "Explorer violation", "increase(seedlog_sim_violations_total[5m])", "0s", "critical"),
    ("throughput_regression", "Throughput regression",
     "sum(rate(seedlog_sim_seeds_total[5m])) / avg_over_time(sum(rate(seedlog_sim_seeds_total[5m]))[7d:1h]) < bool 0.7", "5m", "warning"),
    ("no_leader", "No leader", 'absent(seedlog_raft_role{role="leader"} == 1)', "5s", "critical"),
    ("leader_flapping", "Leader flapping", "increase(seedlog_raft_leader_changes_total[1m]) > bool 3", "0s", "warning"),
    ("apply_lag", "Apply lag", "(seedlog_raft_commit_index - seedlog_raft_applied_index) > bool 1000", "30s", "warning"),
    ("slow_fsync", "Slow fsync",
     "histogram_quantile(0.99, sum by (le) (rate(seedlog_wal_fsync_duration_seconds_bucket[1m]))) > bool 0.05", "1m", "warning"),
    ("error_ratio", "Error ratio",
     'sum(rate(seedlog_kv_requests_total{outcome!="ok"}[1m])) / sum(rate(seedlog_kv_requests_total[1m])) > bool 0.01', "1m", "warning"),
]
y = "apiVersion: 1\ngroups:\n  - orgId: 1\n    name: seedlog\n    folder: SeedLog\n    interval: 10s\n    rules:\n"
for uid, title, expr, dur, sev in alerts:
    y += f"""      - uid: {uid}
        title: {title}
        condition: C
        for: {dur}
        labels:
          severity: {sev}
        noDataState: NoData
        execErrState: Error
        data:
          - refId: A
            relativeTimeRange: {{from: 300, to: 0}}
            datasourceUid: prometheus
            model: {{expr: '{expr}', refId: A}}
          - refId: C
            datasourceUid: __expr__
            model: {{type: threshold, expression: A, refId: C, conditions: [{{evaluator: {{type: gt, params: [0]}}}}]}}
"""
w("deploy/grafana/alerts/seedlog.yaml", y)

nodes = "".join(f"""  node{i}:
    build: {{context: ../.., dockerfile: Dockerfile}}
    command: ["node", "--id", "{i}", "--peers", "node0:7000,node1:7000,node2:7000,node3:7000,node4:7000", "--http", ":8080", "--data", "/data"]
    volumes: ["data{i}:/data"]
    cap_add: [NET_ADMIN]
    ports: ["{8080 + i}:8080"]
""" for i in range(5))
vols = "".join(f"  data{i}: {{}}\n" for i in range(5))
w("deploy/compose/docker-compose.yml", f"""services:
{nodes}  workload:
    build: {{context: ../.., dockerfile: Dockerfile}}
    command: ["workload", "--targets", "node0:8080,node1:8080,node2:8080,node3:8080,node4:8080"]
    depends_on: [node0, node1, node2, node3, node4]
volumes:
{vols}""")

w("Dockerfile", """FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /seedlog ./cmd/seedlog

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /seedlog /seedlog
USER nonroot
ENTRYPOINT ["/seedlog"]
""")

w(".goreleaser.yaml", """version: 2
builds:
  - main: ./cmd/seedlog
    binary: seedlog
    env: [CGO_ENABLED=0]
    goos: [linux, windows, darwin]
    goarch: [amd64, arm64]
archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
checksum:
  name_template: checksums.txt
""")
print("written")
