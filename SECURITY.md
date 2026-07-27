# Security policy

## Reporting a vulnerability

Please report security issues privately, **not** as a public issue.

Use GitHub's private vulnerability reporting: go to the
[Security tab](https://github.com/jonascript/ike/security) and choose *Report a
vulnerability*. That creates a private thread visible only to the maintainer.

Please include what you ran, what happened, and why you think it is a security
problem rather than a bug. A proof of concept helps. Redact your own task titles
from any output you attach.

This is a small hobby project maintained by one person, so response times are
best effort — expect days rather than hours. You will get an acknowledgement,
and credit in the release notes if you would like it.

## What is in scope

ike is a local-first, single-user tool. It has no network listener, no server,
and no authentication. The interesting parts of its threat model are:

- **The data file.** It is created `0600` inside a `0700` directory. Anything
  that leaks task contents to other local users, or that writes outside the
  configured data file, is in scope. So is any path where a crash or an
  interrupted write can lose or corrupt the matrix.
- **Terminal output.** Task titles and quadrant labels are untrusted text —
  particularly when they arrive over MCP, since an agent can be prompt-injected
  by anything it reads. A title that can inject escape sequences into `ike list`
  or the TUI is in scope: it lets stored data misrepresent itself in the very
  view a human uses to audit an agent. Both input validation and output
  sanitization exist for this; a bypass of either is a valid report.
- **The MCP gate.** `ike mcp` refuses to start while access is off, and the gate
  is re-checked on every read and write, so `ike mcp disable` revokes a session
  that is already connected. A way to serve the matrix over MCP while the
  setting is off — or for an MCP tool to change that setting — is in scope.

## What is not in scope

- **The MCP gate is a consent mechanism, not a security boundary.** It cannot
  restrain an agent that already has shell access, which can read the data file
  directly, run `ike list`, or run `ike mcp enable` itself. Reports that an agent
  with a shell can reach your tasks describe the design, not a flaw.
- **`IKE_DATA_FILE` pointing somewhere unwise.** You set it, with your own
  privileges. It is validated (absolute, existing parent) to prevent mistakes,
  not to contain a hostile value.
- **Anyone who can already write your data file or run your binary.** They can
  edit your tasks; there is no privilege boundary between you and your own files.
- **Concurrency on network filesystems.** The locking relies on advisory
  `flock`, which is unreliable on NFS and some FUSE mounts. This is documented
  rather than fixed.
- Vulnerabilities in Go or in dependencies — report those upstream. `govulncheck`
  runs in CI; if it flags something reachable here, an issue is fine.

## Supported versions

Only the latest release is supported. Fixes ship in a new release rather than
as patches to older tags.
