# Demo Transcript

This transcript uses the safe sample vault in `examples/sample-vault/`.
It shows the intended MCP flow: broad questions start at the Quick Index,
then the assistant reads the specific target doc before answering.

## Setup

```bash
SKR_VAULT_PATH=examples/sample-vault uv run severino-knowledge-router
```

## Broad Operational Question

User:

```text
How do I expose a new homelab service over HTTPS?
```

MCP client action:

```text
Read resource: vault://quick-index
```

Relevant Quick Index row:

```text
Add HTTPS to an internal service -> rb-add-nginx-proxy-host
```

MCP client action:

```text
Read resource: vault://doc/rb-add-nginx-proxy-host
```

Assistant answer:

```text
Use the Add Nginx Proxy Host runbook.

1. Open Nginx Proxy Manager.
2. Add a proxy host for <service>.homelab.
3. Set the forward hostname and port for the service container.
4. Attach the matching certificate.
5. Save and verify https://<service>.homelab.
```

## Specific Runbook Question

User:

```text
What's the cert generation runbook?
```

MCP client action:

```text
Call tool: find_runbook("cert generation")
```

Expected top hit:

```text
rb-generate-homelab-cert
```

MCP client action:

```text
Read resource: vault://doc/rb-generate-homelab-cert
```

Assistant answer:

```text
Run:

cd ~/Documents/Code/Projects/cert-generator
./cert-gen <service>.homelab

Then enter the CA key passphrase when prompted and confirm the certificate
output files were created.
```

## Secret-Adjacent Doc

User:

```text
Show me the offline CA doc.
```

MCP client action:

```text
Read resource: vault://doc/infra-offline-ca
```

Expected behavior:

```text
The MCP returns an advisory plus metadata. It does not return the body by
default because the doc is marked secret_adjacent.
```

If the user explicitly needs the body and accepts the risk, the client can
fall back to:

```text
Call tool: read_doc("infra-offline-ca", include_secret_adjacent=True)
```
