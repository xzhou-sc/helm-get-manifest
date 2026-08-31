# helm-get-manifest

`helm-get-manifest` filters the rendered manifest stored for an installed Helm release.

It adds source-aware querying similar to `helm template --show-only`, but operates on
`helm get manifest` output rather than re-rendering the chart.

```bash
helm get-manifest my-release deployment.yaml --clean | yq '.spec'
```

## Why

`helm get manifest` returns everything a release rendered, with no way to narrow it to
one template. `helm template --show-only` can do that, but only by re-rendering the
chart locally.

This plugin never re-renders. It reads what Helm stored for the release, which is what
you want when:

- the local chart is unavailable, or its version has moved on;
- the local values no longer match what was installed;
- the release was installed by another system;
- you are inspecting an older revision;
- reproducing the original render environment is inconvenient or impossible.

It replaces the usual ad-hoc `awk`/`yq` filter over `# Source:` comments.

## Install

```bash
helm plugin install https://github.com/xzhou-sc/helm-get-manifest
```

Release archives ship a prebuilt binary for Linux, macOS and Windows on amd64 and arm64,
so no toolchain is needed. Installing from a git checkout instead builds from source and
requires Go.

### Helm 4 and signature verification

Helm 4 verifies plugin signatures by default, and a git source cannot carry one. If you
install straight from the repository rather than from a release, skip verification:

```bash
helm plugin install https://github.com/xzhou-sc/helm-get-manifest --verify=false
```

Released archives are signed when a signing key is configured, and verify normally.

## Usage

```
helm get-manifest RELEASE [SOURCE] [flags]
```

`SOURCE` selects the documents one template produced. Pass it positionally, right after
the release name.

| Flag | Description |
| --- | --- |
| `-s`, `--source SOURCE` | Same as passing `SOURCE` positionally. |
| `-c`, `--clean` | Strip Helm source comments and the leading document separator, for piping. |
| `-l`, `--list` | List the distinct sources, one per line. |
| `-r`, `--revision N` | Inspect a specific stored revision. |
| `-n`, `--namespace` | Namespace scope for this request. |
| `-k`, `--kube-context` | Name of the kubeconfig context to use. |

Namespace and context default to the ones Helm was invoked with, so the plugin resolves
the same release `helm get manifest` would.

### Examples

The whole stored manifest, byte for byte what `helm get manifest` returns:

```bash
helm get-manifest my-release
```

Just the documents one template produced:

```bash
helm get-manifest my-release deployment.yaml
```

Pipe-friendly output:

```bash
helm get-manifest my-release deployment.yaml -c | yq '.spec.template.spec.containers'
helm get-manifest my-release deployment.yaml -c | kubectl diff -f -
```

Find the source you want:

```bash
helm get-manifest my-release -l | grep secret
helm get-manifest my-release -l | fzf
```

Inspect a past revision:

```bash
helm get-manifest my-release deployment.yaml -r 12
```

### Source paths

A source can be given as the full path Helm records, or as any trailing part of it that
is unambiguous. The `.yaml`/`.yml` extension can be left off, since charts are
inconsistent about which they use:

```bash
helm get-manifest my-release deployment
helm get-manifest my-release deployment.yaml
helm get-manifest my-release templates/deployment.yaml
helm get-manifest my-release my-chart/templates/deployment.yaml
```

Suffixes match whole path elements, so `config` does not match `external-config.yaml`.
An exact path always wins, so a full path is never ambiguous. An extension you do type
is respected: `service.yaml` will not match `service.yml`.

Charts often reuse template names across subcharts. When a shorthand matches more than
one source, the plugin says so rather than picking one — add enough leading path to make
it unique:

```
$ helm get-manifest my-release secret.yaml
helm get-manifest: source is ambiguous: secret.yaml

matches:
  my-chart/templates/secret.yaml
  my-chart/charts/sub/templates/secret.yaml

$ helm get-manifest my-release sub/templates/secret.yaml   # resolves
```

Use `-l` to see the exact strings. If nothing matches, near matches are suggested on
stderr:

```
$ helm get-manifest my-release wrong-chart/templates/configmap.yaml
helm get-manifest: source not found: wrong-chart/templates/configmap.yaml

available matches:
  my-chart/templates/configmap.yaml
```

## Behaviour

**Output is preserved.** Without `--clean`, selected documents are emitted exactly as
Helm stored them: no reordering, re-indenting, re-quoting or comment loss. The plugin is
a filter, not a formatter. With no `--source`, output is byte-identical to
`helm get manifest`.

**`--clean` does the minimum.** It removes Helm's `# Source:` line and the leading `---`,
and nothing else. Unrelated comments survive. Separators *between* documents are kept, so
a template that renders several resources still yields a valid multi-document stream:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: foo-a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: foo-b
```

**Document boundaries are real.** A `---` inside a block or quoted scalar is content, not
a separator, and a `# Source:` inside a resource body is data, not provenance:

```yaml
data:
  script: |
    echo hello
    ---
    echo world
```

stays one document.

**Unix conventions.** Selected manifests go to stdout, diagnostics to stderr, and a closed
pipe (`| head`) exits quietly.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `2` | Invalid usage. |
| `3` | Helm failed to return the release manifest. |
| `4` | The requested source was not found. |
| `5` | The requested source was ambiguous. |

## Development

```bash
go test ./...
go build -o bin/get-manifest ./cmd/get-manifest
helm plugin install .
```

Parser and CLI tests run from fixtures and need no cluster.

### Releasing

Bump `version` in `plugin.yaml`, then push a matching tag:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The release workflow cross-compiles every platform, packages them with
`helm plugin package`, and attaches the archive to a GitHub release. It refuses to
publish if the tag and `plugin.yaml` disagree.

To publish signed archives, set the `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE` and
`GPG_KEY_NAME` repository secrets. Without them the release is published unsigned and
users need `--verify=false`.

## License

Apache-2.0
