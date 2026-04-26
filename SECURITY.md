# Security policy

## Reporting a vulnerability

If you find a security issue in MeterLogger, do not open a public GitHub issue.

Report it privately through GitHub's security advisory form:

https://github.com/yottabytesolutions/meterlogger/security/advisories/new

If GitHub Security Advisories are not available to you, send an email to the security contact listed on the Yottabyte Solutions website. Use the subject line `meterlogger: security report`. If you can, encrypt the report with the published PGP key.

Please include:

- A clear description of the issue and its impact.
- The version, commit SHA, or container image digest you tested.
- The configuration that reproduces the problem, with secrets redacted.
- Any proof-of-concept input, logs, or scripts.
- Whether the issue has been disclosed to anyone else.

## What to expect

- We confirm receipt within 5 business days.
- We give an initial assessment within 10 business days.
- We agree on a coordinated disclosure timeline. The default target is a fix within 90 days of the report.
- We credit reporters in the published advisory unless asked otherwise.

## Scope

In scope:

- The MeterLogger binary and container image, on supported platforms.
- The HTTP endpoints exposed by the health server (`/healthz`, `/readyz`, `/metrics`).
- Configuration parsing, including file and environment variable handling.
- Sink and source adapters in this repository, including database connection handling and credential lifecycle.

Out of scope:

- Vulnerabilities in upstream dependencies that have an upstream fix not yet released. Report those upstream and let us know so we can track them.
- Configurations that intentionally expose the health server, database credentials, or serial devices to untrusted networks.
- Issues that require already having root on the host.
- Findings from automated scanners with no exploitable impact.

## Hardening notes for operators

- Run the container as a non-root user. The published image already drops to user `minion`.
- Bind the health server to an internal network only. It exposes process metrics through `/metrics`.
- Pass database credentials through environment variables or a secret store, not committed config files.
- Revoke and rotate any credential that has appeared in a log.
- Pin the container image by digest in production.
