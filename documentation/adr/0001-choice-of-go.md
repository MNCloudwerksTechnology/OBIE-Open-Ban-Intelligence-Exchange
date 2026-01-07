# ADR 0001: Choice of Go as Primary Language

## Status
Accepted

## Context
OBIE (Open Blockchain Intelligence Exchange) requires a language that can handle high-performance networking, provide strong security guarantees, and facilitate the deployment of decentralized nodes across diverse environments. 

The system relies heavily on:
- **P2P Networking:** Leveraging `libp2p` for decentralized communication.
- **System Integration:** Interfacing with Linux kernel features like `nftables` and `eBPF`.
- **Deployment Ease:** Distributing binaries to various platforms without complex dependency chains.
- **Concurrency:** Handling multiple asynchronous event streams (indicators, heartbeats, policy evaluations).

## Decision
We have chosen **Go (Golang)** as the primary programming language for the OBIE reference implementation.

## Rationale
- **`libp2p` Maturity:** Go has the most mature and feature-complete implementation of the `libp2p` stack, which is critical for OBIE's decentralized architecture.
- **Static Binaries:** Go produces statically linked binaries, simplifying deployment in varied Linux environments (firewalls, edge routers, servers) without requiring a runtime or specific library versions.
- **Concurrency Model:** Go's goroutines and channels provide a safe and efficient way to handle the high-concurrency requirements of a distributed reputation system.
- **Performance:** Go offers near-native performance while maintaining memory safety, which is essential for processing high-rate network signals and interacting with `eBPF`.
- **Developer Productivity:** The language's simplicity, strong standard library, and excellent tooling (testing, profiling) accelerate development and improve maintainability.
- **Security:** Go's memory safety features help mitigate common vulnerabilities like buffer overflows, which is paramount for a security-focused tool.

## Consequences
- **Positive:**
    - Faster development of the P2P mesh networking layer.
    - Simplified CI/CD and distribution processes.
    - Robust handling of concurrent signal processing.
- **Negative:**
    - Integration with `eBPF` may require C shims or specific Go libraries (e.g., `cilium/ebpf`), though this is well-supported in the Go ecosystem.
    - Binaries are larger than those produced by languages like C or Rust (though still manageable).
