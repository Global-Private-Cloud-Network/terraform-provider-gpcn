# Diagrams

Each diagram has a Mermaid source (`.mmd`) and a rendered image (`.svg`). The
docs embed the `.svg`. Edit the `.mmd`, then re-render.

## Render

Run from the `docs-for-developers/` directory:

```bash
npx -y @mermaid-js/mermaid-cli -i diagrams/async-flow.mmd -o diagrams/async-flow.svg
npx -y @mermaid-js/mermaid-cli -i diagrams/layers.mmd -o diagrams/layers.svg
```

Commit both the `.mmd` source and the `.svg` output.

## Files

| Source | Image | Shows |
| ------ | ----- | ----- |
| `async-flow.mmd` | `async-flow.svg` | The async create/read/update/delete request flow. |
| `layers.mmd` | `layers.svg` | The three-layer package structure. |
