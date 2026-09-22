# Security policy

## Supported versions

Security fixes go into the latest minor release of the provider published on the [Terraform Registry](https://registry.terraform.io/providers/Global-Private-Cloud-Network/gpcn/latest). Upgrade to the latest release to receive them.

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately to **security@gpcn.com**, or use GitHub's private vulnerability reporting on this repository if it is enabled. Include the provider version, the Terraform version, what you observed, and how to reproduce it. If the problem is in the GPCN API rather than the provider, say so; we will route it.

You will receive an acknowledgement within three business days and a status update when the report has been assessed. We ask that you give us reasonable time to fix the problem before any public disclosure, and we will credit you in the release notes unless you prefer otherwise.

## What counts

- A way to make the provider leak credentials, for example an API key appearing in logs, state, or diagnostics beyond where Terraform itself stores it.
- A way to make the provider destroy or alter resources the configuration did not ask it to.
- Dependency vulnerabilities that are exploitable through the provider. CI runs `govulncheck` and CodeQL on every pull request and weekly.

Behavioural bugs without a security impact belong in a normal issue.
