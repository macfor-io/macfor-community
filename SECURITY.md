# Security Policy

## Supported versions

macfor is in early public release. Security fixes will be applied to the
latest minor version on the `main` branch. Pre-1.0, no long-term branches are
maintained.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, report privately via one of these channels:

- **Email:** `security@macfor.io`
- **GitHub:** [Private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability) on this repository

Please include:

- A description of the issue and its potential impact.
- Steps to reproduce, or a proof-of-concept.
- Affected versions or commits.
- Your name/handle for credit (optional).

## What to expect

- Acknowledgement within **3 business days**.
- An initial assessment within **7 business days**.
- Regular updates until the issue is resolved.
- Public disclosure coordinated with you once a fix is available.

## Scope

In scope:

- Bugs in the collector that could lead to evidence tampering, integrity
  failures, or unintended writes to source evidence.
- Path traversal, arbitrary file read/write, or privilege escalation in the
  evidence container or extraction logic.
- Parser vulnerabilities (e.g. crashes or code execution on malformed input).
- Dependency vulnerabilities that are exploitable in the context of macfor.

Out of scope:

- Missing hardening that does not correspond to a concrete vulnerability.
- Attacks that require the attacker to already have root on the investigator
  machine (macfor operates as a privileged tool by design).
- Social-engineering of maintainers or contributors.

## Handling sensitive artifacts

macfor collects data that is often sensitive (shell history, browser state,
keychain metadata, etc.). If you encounter a potential issue while running
macfor against real evidence, please redact identifying information from any
reports.
