# Requirements: verification tooling (REQ-TRC)

The traceability check is itself part of the evidence, so it has requirements.

- **REQ-TRC-001** Every requirement defined under `verification/requirements/` is verified by at least one test that cites its ID with a `// Verifies:` comment.
- **REQ-TRC-002** A `// Verifies:` comment may only cite requirement IDs that are defined, and must be followed by a Test function.
- **REQ-TRC-003** A requirement ID is defined exactly once.
