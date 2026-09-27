# ADR 0005: Node identity key

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1652](https://openproject.niew.dev/work_packages/1652)

## Context

ADR 0001 fixes one Ed25519 key per node, used both as the libp2p peer ID
and as the event-signing key (ADR 0004). The key must survive restarts,
because trust weights are assigned per peer ID, and it must never be
readable by other users of the host. The mesh (go-libp2p) arrives with a
later work package and must be able to use the same key file unchanged.

## Decision

- **Location:** `<node.state_dir>/node.key`. `obied` generates the key on its
  first start and loads it on every later start, before any subsystem
  starts; if the key cannot be loaded, `obied` does not start.
  `obied keygen [--force]` creates it offline; `obied identity` shows it
  offline.
- **File format:** the libp2p marshaled private key, i.e. exactly the bytes
  `crypto.MarshalPrivateKey` of go-libp2p writes for an Ed25519 key: the
  protobuf message `PrivateKey {Type: Ed25519 (1), Data: seed ‖ public key}`
  (68 bytes, no text encoding). It is encoded and decoded in-house in
  `internal/identity`, like the peer ID in `pkg/obieproto`, so the module
  does not depend on go-libp2p before the mesh needs it. Reading is strict:
  any other length or key type, and a public half that does not match the
  seed, is reported as a corrupted key file — never repaired.
- **Permissions:** the key file is written with mode 0600; a missing state
  directory is created with mode 0700. An existing state directory keeps its
  mode but must not be writable by group or others (e.g. 0700, 0750), since
  they could delete the key and `obied` would then silently generate a new
  identity. On load, `obied` refuses such a directory, and a key file that
  is not a regular file (symlinks are not followed), that grants any
  permission to group or others, or that is not owned by the effective user;
  the error names the fix (`chmod 700`, `chmod 600`, `chown`). Type, mode
  and owner are checked on the opened file, so the file cannot be swapped
  between check and read. Keys created with `obied keygen` must therefore be
  created as the service user.
- **Atomic write:** the key is written to a temporary file in the same
  directory, synced, and then moved into place — with a hard link when an
  existing key must not be replaced (so a concurrent writer cannot be
  overwritten either), with a rename for `--force` — and the directory is
  synced. A crash never leaves a partial `node.key`. The state directory's
  file system must therefore support hard links (every local Linux file
  system does); otherwise creating the key fails with an error saying so.
- **Fingerprint:** `SHA256:` followed by the unpadded standard base64 of the
  SHA-256 hash of the raw 32-byte public key, in the style of OpenSSH. It is
  shown next to the peer ID so operators can compare keys at a glance.
- **Access from other subsystems:** `internal/identity.Identity` offers only
  `PeerID()`, `PublicKey()` and `Sign([]byte)`. The private key never leaves
  the package; the concrete type prints as its peer ID, so logging it cannot
  leak the key.
- **Admin API:** `GET /v1/identity` returns `{"peer_id", "fingerprint"}`,
  shown by `obiectl identity`. Neither the API nor any command outputs the
  private key.

## Consequences

- Replacing `node.key` (`obied keygen --force`) changes the peer ID; every
  peer that assigned a trust weight to the old one must update it.
- The file stays readable by go-libp2p's `crypto.UnmarshalPrivateKey`, so the
  mesh can adopt it without a migration.
- `obieproto.Sign` (ADR 0004) takes an `ed25519.PrivateKey`, which
  `Identity` withholds. The work package that publishes events adds a
  variant of `Sign` that takes a signer offering `PeerID()` and `Sign()`,
  instead of reaching for the private key.
- Key rotation statements and organisational identity are out of scope for
  v0.1 and listed as future work in `ARCHITECTURE.md`.
