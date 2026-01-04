# Embedded NATS with JetStream for holos-console

**Date:** 2026-01-04
**Author:** Research Analysis
**Context:** Staff-level platform engineering evaluation for NASDAQ 100 enterprise deployment

## Executive Summary

This report evaluates embedding NATS with JetStream into the holos-console Go executable for deployment as a 3-pod StatefulSet in Kubernetes. The primary use case is session management and OIDC token storage for a platform supporting ~100 engineers across 6 teams building Agentic AI applications.

**Recommendation:** Proceed with embedded NATS using JetStream KV for session/token storage, but with **mandatory mitigations** for data persistence issues identified by the [Jepsen analysis](https://jepsen.io/analyses/nats-2.12.1). The approach is viable for this specific use case (ephemeral session data with re-authentication fallback) but would **not be appropriate** for data requiring guaranteed durability.

**Key Risk:** NATS JetStream has documented data loss scenarios under certain failure conditions. For session tokens specifically, this is an acceptable risk because users can re-authenticate. Required mitigations include synchronous fsync, read-after-write verification, and chaos testing before production.

## Table of Contents

1. [Background and Prior Art](#background-and-prior-art)
2. [Technical Architecture](#technical-architecture)
3. [Deployment Considerations](#deployment-considerations)
4. [Security Analysis](#security-analysis)
5. [Alternatives Considered](#alternatives-considered)
6. [Risk Assessment](#risk-assessment)
7. [Required Mitigations for Data Persistence](#required-mitigations-for-data-persistence)
8. [Applicability to Our Use Case](#applicability-to-our-use-case)
9. [Recommendations](#recommendations)

---

## Background and Prior Art

### Machine Room Project (Choria)

R.I. Pienaar (ripienaar) created the [machine-room](https://github.com/choria-io/machine-room) project as part of the Choria ecosystem. It provides "an integrated managed SaaS backend using Choria Autonomous Agents" - a framework for building SaaS infrastructure with agents that deploy at customer sites and communicate with a cloud backend.

**Key architectural patterns from machine-room:**

1. **Hub-and-spoke SaaS model**: Customer-deployed agents (spokes) connect to a cloud backend (hub)
2. **NATS as messaging backbone**: Handles provisioning, streaming, security, and reconciliation loops
3. **Embedded Choria Broker**: Contains an embedded NATS instance managing distributed systems concerns

**Embedded Choria Sample App** ([ripienaar/embedded-choria-sample](https://github.com/ripienaar/embedded-choria-sample)) demonstrates:

```go
cfg, err = choria.NewConfig("/dev/null")
cfg.Choria.MiddlewareHosts = []string{"demo.nats.io:4222"}
cfg.RegisterInterval = 60
cfg.MainCollective = "acme"
```

The pattern separates concerns: a main application performs primary work while a background Choria/NATS server handles distributed management tasks.

### Choria's Approach to Embedded NATS

Choria has successfully run production workloads with embedded NATS at significant scale:

- 5,500 nodes on a single 2 CPU VM with 4GB RAM
- 50,000 nodes on a single VM with NATS using 1-1.5GB RAM at peak
- 2,000 nodes on a single NATS server consuming 300MB RAM

This demonstrates that embedded NATS can handle substantial production workloads.

---

## Technical Architecture

### Embedding NATS in Go

NATS can be embedded directly into Go applications:

```go
import (
    natsserver "github.com/nats-io/nats-server/v2/server"
    "github.com/nats-io/nats.go"
)

serverOpts := &natsserver.Options{
    ServerName:      "holos-console-1",
    Port:            4222,
    JetStream:       true,
    JetStreamDomain: "holos",
    StoreDir:        "/data/jetstream",
}

ns, err := natsserver.NewServer(serverOpts)
ns.Start()

// Connect using in-process connection (no network overhead)
nc, err := nats.Connect(ns.ClientURL())
```

**Advantages:**
- Single binary deployment
- No external dependencies for messaging
- In-process communication for local operations
- Simplified operational model

**Disadvantages:**
- Application lifecycle tied to messaging infrastructure
- Configuration complexity embedded in application
- Debugging more complex (interleaved concerns)

### JetStream Key/Value Store

JetStream provides a persistent KV store abstraction on top of streams:

```go
js, _ := nc.JetStream()

// Create a KV bucket for session storage
kv, _ := js.CreateKeyValue(&nats.KeyValueConfig{
    Bucket:      "sessions",
    Description: "OIDC session and token storage",
    TTL:         24 * time.Hour,      // Automatic expiration
    History:     1,                    // Keep only latest value
    Replicas:    3,                    // Replicate across cluster
})

// Store encrypted session data
kv.Put("session:abc123", encryptedTokenData)

// Retrieve session
entry, _ := kv.Get("session:abc123")
```

**Key Features:**
- Immediate consistency (not eventual)
- Configurable TTL for automatic expiration
- History tracking per key
- Replication across cluster nodes

### Clustering for StatefulSet Deployment

**Critical Requirement:** JetStream clustering requires at least 3 nodes and uses RAFT consensus.

```yaml
# NATS cluster configuration for 3-pod StatefulSet
cluster:
  name: holos-cluster
  routes:
    - nats://holos-console-0.holos-console-headless:6222
    - nats://holos-console-1.holos-console-headless:6222
    - nats://holos-console-2.holos-console-headless:6222

jetstream:
  store_dir: /data/jetstream
  max_memory_store: 256MB
  max_file_store: 1GB
```

**Quorum Math:**
- 3 nodes → quorum of 2 (tolerates 1 failure)
- 5 nodes → quorum of 3 (tolerates 2 failures)

**Known Issue:** [GitHub Issue #4794](https://github.com/nats-io/nats-server/issues/4794) documents challenges getting JetStream clusters working with embedded NATS servers. The core issue is establishing proper inter-node connectivity before JetStream operations.

---

## Deployment Considerations

### Kubernetes StatefulSet Configuration

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: holos-console
spec:
  serviceName: holos-console-headless
  replicas: 3
  selector:
    matchLabels:
      app: holos-console
  template:
    metadata:
      labels:
        app: holos-console
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: holos-console
              topologyKey: kubernetes.io/hostname
      containers:
        - name: holos-console
          image: holos-console:latest
          env:
            - name: GOMEMLIMIT
              value: "6GiB"  # 80-90% of container limit
            - name: JS_KEY
              valueFrom:
                secretKeyRef:
                  name: holos-secrets
                  key: jetstream-encryption-key
          ports:
            - name: https
              containerPort: 8443
            - name: nats-client
              containerPort: 4222
            - name: nats-cluster
              containerPort: 6222
          volumeMounts:
            - name: data
              mountPath: /data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: premium-rwo
        resources:
          requests:
            storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: holos-console-headless
spec:
  clusterIP: None
  selector:
    app: holos-console
  ports:
    - name: nats-cluster
      port: 6222
```

### Critical Configuration Points

1. **Pod Anti-Affinity**: Required to spread pods across nodes for true HA
2. **Headless Service**: NATS requires direct pod-to-pod communication
3. **GOMEMLIMIT**: Essential for containerized Go - prevents OOM
4. **Persistent Volumes**: Use fast SSD storage (avoid NFS/NAS)
5. **Network**: Ensure no load balancer between cluster nodes

### Resource Recommendations

For production JetStream with 3-node cluster:

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 2 cores | 4 cores |
| Memory | 4 GiB | 8 GiB |
| Storage | 10 GiB SSD | 20 GiB SSD |

---

## Security Analysis

### OIDC Token Storage Security

**Threat Model:**

| Threat | Mitigation |
|--------|------------|
| Token theft from storage | Encryption at rest (JetStream native or client-side) |
| Token theft in transit | TLS between all NATS nodes and clients |
| Unauthorized access | NATS authentication/authorization |
| Token replay | Short TTLs, token binding to session |
| Cluster compromise | Kubernetes RBAC, network policies |

### Encryption at Rest

JetStream supports native encryption:

```go
jetstream {
    key: $JS_KEY  // Environment variable, 32+ bytes recommended
    cipher: chacha  // or aes
}
```

**What gets encrypted:**
- Message blocks (headers and payloads)
- Stream metadata files
- Consumer metadata files

**Recommendation:** Use client-side encryption for tokens before storing in KV. This provides defense-in-depth and allows per-token encryption keys if needed.

```go
// Example: Client-side encryption before KV storage
type SecureTokenStore struct {
    kv     nats.KeyValue
    cipher cipher.AEAD
}

func (s *SecureTokenStore) StoreTokens(sessionID string, tokens *OIDCTokens) error {
    plaintext, _ := json.Marshal(tokens)
    nonce := make([]byte, s.cipher.NonceSize())
    io.ReadFull(rand.Reader, nonce)
    ciphertext := s.cipher.Seal(nonce, nonce, plaintext, nil)
    _, err := s.kv.Put("session:"+sessionID, ciphertext)
    return err
}
```

### NATS Authentication Options

For internal cluster communication:

1. **TLS Mutual Authentication**: Recommended for cluster routes
2. **NKeys**: Ed25519-based authentication for clients
3. **JWT with Accounts**: Decentralized auth with account isolation

**Recommended Approach for holos-console:**

```go
// TLS for cluster routes + system account for internal use
opts := &natsserver.Options{
    TLS:           true,
    TLSCert:       "/certs/server.crt",
    TLSKey:        "/certs/server.key",
    TLSCaCert:     "/certs/ca.crt",
    TLSVerify:     true,
    Cluster: natsserver.ClusterOpts{
        TLSConfig: clusterTLSConfig,
    },
    SystemAccount: "SYS",
}
```

### Token Storage Best Practices

Following [CyberArk](https://docs.cyberark.com/identity-administration/latest/en/content/developer/oidc/tokens/token-storage.htm) and [Auth0](https://auth0.com/docs/secure/tokens/token-best-practices) recommendations:

1. **Never store tokens in browser-accessible storage**
2. **Use HTTP-only cookies for session identifiers**
3. **Store actual tokens server-side only**
4. **Encrypt tokens at rest**
5. **Short expiration times with refresh rotation**
6. **Bind tokens to session/device**

The holos-console architecture (BFF pattern) aligns perfectly with these recommendations - the UI never sees raw tokens.

---

## Alternatives Considered

### Option 1: Embedded NATS (Proposed)

**Pros:**
- Single binary deployment
- No external dependencies
- Low operational overhead
- Good fit for edge/hybrid deployments

**Cons:**
- Clustering complexity with embedded servers
- Lifecycle coupling
- Less operational visibility

### Option 2: External NATS Cluster

**Pros:**
- Production-proven clustering
- Operational separation
- Standard Helm chart deployment
- Better observability

**Cons:**
- Additional infrastructure to manage
- Network latency for local operations
- More moving parts

### Option 3: Synadia Cloud (NGS)

**Pros:**
- Fully managed with 99.99% SLA
- Global multi-region
- Enterprise support
- Zero operational overhead

**Cons:**
- External dependency
- Data residency considerations
- Cost at scale
- Vendor lock-in concerns

### Option 4: Redis/PostgreSQL for Session Storage

**Pros:**
- Well-understood technology
- Proven at scale
- Rich ecosystem

**Cons:**
- No built-in messaging
- Additional infrastructure
- Different technology for messaging vs storage

### Comparison Matrix

| Criteria | Embedded NATS | External NATS | Synadia Cloud | Redis/PG |
|----------|---------------|---------------|---------------|----------|
| Operational Complexity | Medium | High | Low | Medium |
| Innovation Speed | High | Medium | High | Medium |
| Security Control | High | High | Medium | High |
| Cost | Low | Medium | High | Medium |
| Scalability | Medium | High | High | High |
| Messaging + Storage | Yes | Yes | Yes | No |
| Enterprise Readiness | Medium | High | High | High |

---

## Risk Assessment

### Critical: Jepsen Analysis Findings (NATS 2.12.1)

The [Jepsen analysis of NATS 2.12.1](https://jepsen.io/analyses/nats-2.12.1) uncovered **serious data persistence issues** that directly impact our use case. This section provides a detailed analysis and required mitigations.

#### Bugs Identified by Jepsen

| Bug | Issue | Impact | Status |
|-----|-------|--------|--------|
| Stream Deletion on Crash | Process crashes caused complete stream/data loss | CRITICAL | Fixed in 2.10.23 |
| Block File Corruption (#7549) | Single-bit errors or truncation caused loss of up to 679,153 acknowledged writes | CRITICAL | Open |
| Snapshot File Corruption (#7556) | Corrupted snapshots caused nodes to delete all data, even with minority corruption | CRITICAL | Open |
| Lazy fsync Policy (#7564) | Default 2-minute fsync interval means acknowledged data can be lost | HIGH | By Design |
| Single OS Crash Split-Brain (#7567) | OS crash + network delay caused persistent replica divergence | HIGH | Open |

#### Critical Concern: Lazy fsync by Default

**This is the most significant finding for our use case.**

NATS JetStream acknowledges messages immediately but only flushes to disk every 2 minutes by default. This means:

> "Once a JetStream client's publish request is acknowledged by the server, that message has been successfully persisted" — **this guarantee is violated**.

**Impact on OIDC Token Storage:**
- User authenticates, tokens stored, acknowledgment received
- Node experiences power failure 30 seconds later
- Tokens are **permanently lost** despite successful acknowledgment
- User must re-authenticate (acceptable for session, but violates expected semantics)

#### Replica Divergence Concern

Jepsen found scenarios where different nodes retain different sets of acknowledged messages **even after cluster recovery**. This creates a "split-brain" condition that persists silently.

**Impact on Token Storage:**
- Session token stored on node A, replicated to B
- Partition + recovery causes nodes to have different views
- User routed to node with missing token experiences unexpected logout
- Debugging is difficult because cluster appears healthy

#### Risk Rating Adjustment

Based on Jepsen findings, we must **upgrade "Data Loss on Storage Failure" from Medium to CRITICAL**.

### High Risk Items

1. **Embedded Cluster Formation**
   - **Risk:** JetStream cluster may fail to form properly with embedded servers
   - **Mitigation:** Extensive testing, fallback to external NATS if needed
   - **Evidence:** [GitHub Issue #4794](https://github.com/nats-io/nats-server/issues/4794)

2. **Quorum Loss During Scaling**
   - **Risk:** Pod disruption may cause temporary unavailability
   - **Mitigation:** PodDisruptionBudgets, graceful shutdown handlers
   - **Evidence:** Kubernetes autoscaling can cause quorum issues

3. **CRITICAL: Acknowledged Data Loss (Jepsen)**
   - **Risk:** Acknowledged writes may be lost due to lazy fsync, file corruption, or replica divergence
   - **Mitigation:** See [Required Mitigations](#required-mitigations-for-data-persistence) below
   - **Evidence:** [Jepsen NATS 2.12.1 Analysis](https://jepsen.io/analyses/nats-2.12.1)

### Medium Risk Items

4. **Performance Under Load**
   - **Risk:** NATS embedded in same process may compete for resources
   - **Mitigation:** Resource limits, GOMEMLIMIT, separate goroutine pools

5. **Snapshot Corruption Leading to Data Deletion**
   - **Risk:** File corruption on minority of nodes can trigger data deletion
   - **Mitigation:** Monitor for orphan stream warnings, implement backup strategy
   - **Evidence:** [GitHub Issue #7556](https://github.com/nats-io/nats-server/issues/7556)

### Low Risk Items

6. **NATS Version Upgrades**
   - **Risk:** Breaking changes in embedded NATS
   - **Mitigation:** Pin versions, comprehensive testing

7. **Debugging Complexity**
   - **Risk:** Harder to diagnose issues with embedded server
   - **Mitigation:** Structured logging, metrics, tracing

---

## Required Mitigations for Data Persistence

Based on the Jepsen analysis, the following mitigations are **REQUIRED** before production deployment:

### 1. Enable Synchronous Flush (sync_interval: always)

```yaml
jetstream:
  sync_interval: always  # fsync after every message
```

**Trade-off:** This significantly impacts write throughput (potentially 30%+ reduction based on NATS documentation). For session token storage with low write volume, this is acceptable.

**Configuration in Go:**

```go
opts := &natsserver.Options{
    JetStream: true,
    JetStreamSyncInterval: 0, // 0 = sync on every message
}
```

### 2. Upgrade to Latest Patched Version

Ensure NATS server version >= 2.10.23 to get the fix for "Stream Deletion on Crash".

Monitor these GitHub issues for resolution:
- [#7549 Block File Corruption](https://github.com/nats-io/nats-server/issues/7549)
- [#7556 Snapshot Corruption](https://github.com/nats-io/nats-server/issues/7556)
- [#7567 Split-Brain on OS Crash](https://github.com/nats-io/nats-server/issues/7567)

### 3. Implement Application-Level Verification

**Read-After-Write Verification:**

```go
func (s *SecureStore) StoreTokensWithVerification(sessionID string, tokens *OIDCTokens) error {
    // Store tokens
    revision, err := s.kv.Put("session:"+sessionID, encryptedData)
    if err != nil {
        return err
    }

    // Verify write by reading back
    entry, err := s.kv.Get("session:" + sessionID)
    if err != nil {
        return fmt.Errorf("verification read failed: %w", err)
    }

    if entry.Revision() != revision {
        return fmt.Errorf("revision mismatch: expected %d, got %d", revision, entry.Revision())
    }

    return nil
}
```

### 4. Session Regeneration Strategy

Design the application to be resilient to token loss:

```go
type SessionManager struct {
    store       *SecureStore
    maxRetries  int
}

func (sm *SessionManager) GetTokensWithFallback(sessionID string) (*OIDCTokens, error) {
    tokens, err := sm.store.GetTokens(sessionID)
    if err != nil {
        // Token not found - trigger re-authentication flow
        // This is acceptable for UI sessions with short TTL
        return nil, ErrSessionExpired
    }
    return tokens, nil
}
```

### 5. External Backup Strategy

Implement periodic backup to an external system for critical data:

```go
func (s *SecureStore) BackupToExternal(ctx context.Context, backup BackupService) error {
    watcher, err := s.kv.WatchAll()
    if err != nil {
        return err
    }
    defer watcher.Stop()

    for entry := range watcher.Updates() {
        if entry == nil {
            break
        }
        if err := backup.Store(entry.Key(), entry.Value()); err != nil {
            log.Printf("backup failed for key %s: %v", entry.Key(), err)
        }
    }
    return nil
}
```

### 6. Monitoring and Alerting

Implement monitoring for Jepsen-identified failure modes:

```go
// Monitor for orphan stream warnings (precursor to data deletion)
type JetStreamMonitor struct {
    logger *slog.Logger
}

func (m *JetStreamMonitor) CheckClusterHealth(js nats.JetStreamContext) error {
    info, err := js.AccountInfo()
    if err != nil {
        return err
    }

    // Alert on stream count changes (potential orphan deletion)
    if info.Streams != expectedStreamCount {
        m.logger.Error("stream count mismatch",
            "expected", expectedStreamCount,
            "actual", info.Streams)
    }

    // Check replica synchronization
    for _, stream := range info.StreamNames() {
        streamInfo, _ := js.StreamInfo(stream)
        if streamInfo.Cluster != nil {
            for _, replica := range streamInfo.Cluster.Replicas {
                if !replica.Current {
                    m.logger.Warn("replica not current",
                        "stream", stream,
                        "replica", replica.Name,
                        "lag", replica.Lag)
                }
            }
        }
    }

    return nil
}
```

### 7. Chaos Testing Before Production

Implement Jepsen-style testing:

```bash
# Test scenarios to run before production:

# 1. Simultaneous power failure (kill -9 all pods)
kubectl delete pods -l app=holos-console --force --grace-period=0

# 2. Storage corruption simulation
# Inject bit errors into .blk files and observe recovery

# 3. Network partition during write
# Use chaos mesh or similar to partition during token storage

# 4. Single node crash with network delay
# Kill one pod while introducing 5s network delay to others
```

---

## Applicability to Our Use Case

### Why These Issues Are Manageable for Session/Token Storage

1. **Ephemeral by Nature**: Session tokens have short TTLs (hours, not months)
2. **Recovery Path Exists**: Users can re-authenticate if tokens are lost
3. **Low Write Volume**: ~100 engineers × ~10 sessions/day = ~1000 writes/day
4. **Not Financial Data**: No regulatory requirement for zero-loss persistence

### When These Issues Would Be Blocking

This architecture would **NOT** be appropriate for:
- Financial transaction records
- Audit logs with compliance requirements
- Any data where loss cannot be recovered by user action
- High-volume write workloads where sync_interval: always is prohibitive

### Revised Risk Acceptance

| Scenario | Probability | Impact | Mitigation | Residual Risk |
|----------|-------------|--------|------------|---------------|
| Token loss after ack | Low (with sync) | Low (re-auth) | sync_interval: always | ACCEPTABLE |
| Cluster split-brain | Low | Medium | Monitoring, alerts | ACCEPTABLE |
| Full data loss | Very Low | Medium | Backups, re-auth | ACCEPTABLE |
| Silent corruption | Low | Medium | Read verification | ACCEPTABLE |

---

## Recommendations

### Primary Recommendation

**Proceed with embedded NATS + JetStream KV for session/token storage**, with the following phased approach:

#### Phase 1: Single-Node Development (1-2 weeks effort)
- Embed NATS server in holos-console
- Implement JetStream KV for session storage
- Add encryption at rest (client-side + server-side)
- Develop comprehensive tests

#### Phase 2: Cluster Testing (1-2 weeks effort)
- Test 3-node embedded cluster formation
- Validate RAFT consensus behavior
- Test failure scenarios (pod restart, node drain)
- Document recovery procedures

#### Phase 3: Production Hardening (1-2 weeks effort)
- Add monitoring and alerting
- Implement graceful shutdown
- Configure PodDisruptionBudgets
- Performance testing under load

### Fallback Strategy

If embedded clustering proves unreliable:

1. **Short-term:** External NATS cluster via Helm chart
2. **Long-term:** Evaluate Synadia Cloud for managed option

### Implementation Guidelines

```go
// Recommended package structure
console/
├── nats/
│   ├── embedded.go      // Embedded server setup
│   ├── cluster.go       // Cluster configuration
│   ├── kv.go            // KV store abstraction
│   └── security.go      // TLS and encryption
├── session/
│   ├── store.go         // Session storage interface
│   ├── tokens.go        // OIDC token handling
│   └── encryption.go    // Client-side encryption
```

### Key Success Metrics

| Metric | Target |
|--------|--------|
| Session storage latency (p99) | < 10ms |
| Cluster formation time | < 30s |
| Recovery from single pod failure | < 60s |
| Token encryption/decryption overhead | < 1ms |

### Enterprise Considerations

For a NASDAQ 100 company supporting Agentic AI:

1. **Compliance:** Ensure encrypted token storage meets data protection requirements
2. **Audit:** Log all token operations for audit trail
3. **Incident Response:** Document procedures for cluster recovery
4. **Capacity Planning:** Plan for 10x growth in session volume
5. **Multi-Region:** Consider future leaf node architecture for geo-distribution

---

## References

### Critical Analysis
- [**Jepsen Analysis: NATS 2.12.1**](https://jepsen.io/analyses/nats-2.12.1) - Independent safety testing revealing data loss scenarios

### NATS Documentation
- [JetStream Clustering](https://docs.nats.io/running-a-nats-service/configuration/clustering/jetstream_clustering)
- [Key/Value Store](https://docs.nats.io/nats-concepts/jetstream/key-value-store)
- [Encryption at Rest](https://docs.nats.io/running-a-nats-service/nats_admin/jetstream_admin/encryption_at_rest)
- [Security](https://docs.nats.io/nats-concepts/security)
- [NATS and Kubernetes](https://docs.nats.io/running-a-nats-service/nats-kubernetes)

### Implementation Examples
- [Choria Machine Room](https://github.com/choria-io/machine-room)
- [Embedded Choria Sample](https://github.com/ripienaar/embedded-choria-sample)
- [NATS JetStream KV in Go](https://shijuvar.medium.com/using-nats-jetstream-key-value-store-in-go-85f88b0848ce)

### Enterprise Resources
- [Synadia Cloud](https://www.synadia.com/cloud)
- [Token Best Practices - Auth0](https://auth0.com/docs/secure/tokens/token-best-practices)
- [Token Storage - CyberArk](https://docs.cyberark.com/identity-administration/latest/en/content/developer/oidc/tokens/token-storage.htm)

### Issues and Discussions
- [JetStream Cluster with Embedded Server #4794](https://github.com/nats-io/nats-server/issues/4794)
- [NATS Kubernetes Helm Chart](https://github.com/nats-io/k8s/blob/main/helm/charts/nats/values.yaml)

### Jepsen-Identified Bugs (Monitor for Fixes)
- [#7549 Block File Corruption](https://github.com/nats-io/nats-server/issues/7549)
- [#7556 Snapshot File Corruption](https://github.com/nats-io/nats-server/issues/7556)
- [#7564 Lazy fsync Policy](https://github.com/nats-io/nats-server/issues/7564)
- [#7567 Single OS Crash Split-Brain](https://github.com/nats-io/nats-server/issues/7567)

---

## Appendix: Sample Implementation

### Embedded NATS Server with JetStream

```go
package nats

import (
    "fmt"
    "time"

    natsserver "github.com/nats-io/nats-server/v2/server"
    "github.com/nats-io/nats.go"
)

type EmbeddedNATS struct {
    server *natsserver.Server
    conn   *nats.Conn
    js     nats.JetStreamContext
}

func NewEmbeddedNATS(cfg Config) (*EmbeddedNATS, error) {
    opts := &natsserver.Options{
        ServerName:      cfg.ServerName,
        Host:            "0.0.0.0",
        Port:            cfg.ClientPort,
        JetStream:       true,
        StoreDir:        cfg.StoreDir,
        JetStreamMaxMemory: cfg.MaxMemory,
        JetStreamMaxStore:  cfg.MaxStore,
    }

    // Configure clustering if enabled
    if cfg.ClusterEnabled {
        opts.Cluster = natsserver.ClusterOpts{
            Name: cfg.ClusterName,
            Host: "0.0.0.0",
            Port: cfg.ClusterPort,
        }
        opts.Routes = cfg.Routes
    }

    // Configure TLS
    if cfg.TLSEnabled {
        opts.TLS = true
        opts.TLSCert = cfg.TLSCert
        opts.TLSKey = cfg.TLSKey
        opts.TLSCaCert = cfg.TLSCACert
        opts.TLSVerify = true
    }

    server, err := natsserver.NewServer(opts)
    if err != nil {
        return nil, fmt.Errorf("failed to create server: %w", err)
    }

    server.ConfigureLogger()
    go server.Start()

    if !server.ReadyForConnections(10 * time.Second) {
        return nil, fmt.Errorf("server failed to start")
    }

    // Connect to embedded server
    nc, err := nats.Connect(server.ClientURL())
    if err != nil {
        server.Shutdown()
        return nil, fmt.Errorf("failed to connect: %w", err)
    }

    js, err := nc.JetStream()
    if err != nil {
        nc.Close()
        server.Shutdown()
        return nil, fmt.Errorf("failed to get JetStream context: %w", err)
    }

    return &EmbeddedNATS{
        server: server,
        conn:   nc,
        js:     js,
    }, nil
}

func (e *EmbeddedNATS) CreateSessionBucket() (nats.KeyValue, error) {
    return e.js.CreateKeyValue(&nats.KeyValueConfig{
        Bucket:      "sessions",
        Description: "OIDC session and token storage",
        TTL:         24 * time.Hour,
        History:     1,
        Replicas:    3, // For clustered deployment
    })
}

func (e *EmbeddedNATS) Shutdown() {
    if e.conn != nil {
        e.conn.Drain()
    }
    if e.server != nil {
        e.server.Shutdown()
        e.server.WaitForShutdown()
    }
}
```

### Secure Token Storage

```go
package session

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/json"
    "fmt"
    "io"
    "time"

    "github.com/nats-io/nats.go"
)

type OIDCTokens struct {
    IDToken      string    `json:"id_token"`
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresAt    time.Time `json:"expires_at"`
}

type SecureStore struct {
    kv     nats.KeyValue
    cipher cipher.AEAD
}

func NewSecureStore(kv nats.KeyValue, key []byte) (*SecureStore, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    return &SecureStore{kv: kv, cipher: gcm}, nil
}

func (s *SecureStore) StoreTokens(sessionID string, tokens *OIDCTokens) error {
    plaintext, err := json.Marshal(tokens)
    if err != nil {
        return fmt.Errorf("marshal tokens: %w", err)
    }

    nonce := make([]byte, s.cipher.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return fmt.Errorf("generate nonce: %w", err)
    }

    ciphertext := s.cipher.Seal(nonce, nonce, plaintext, nil)

    if _, err := s.kv.Put("session:"+sessionID, ciphertext); err != nil {
        return fmt.Errorf("store tokens: %w", err)
    }

    return nil
}

func (s *SecureStore) GetTokens(sessionID string) (*OIDCTokens, error) {
    entry, err := s.kv.Get("session:" + sessionID)
    if err != nil {
        return nil, fmt.Errorf("get tokens: %w", err)
    }

    ciphertext := entry.Value()
    if len(ciphertext) < s.cipher.NonceSize() {
        return nil, fmt.Errorf("ciphertext too short")
    }

    nonce := ciphertext[:s.cipher.NonceSize()]
    ciphertext = ciphertext[s.cipher.NonceSize():]

    plaintext, err := s.cipher.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt tokens: %w", err)
    }

    var tokens OIDCTokens
    if err := json.Unmarshal(plaintext, &tokens); err != nil {
        return nil, fmt.Errorf("unmarshal tokens: %w", err)
    }

    return &tokens, nil
}

func (s *SecureStore) DeleteTokens(sessionID string) error {
    return s.kv.Delete("session:" + sessionID)
}
```
