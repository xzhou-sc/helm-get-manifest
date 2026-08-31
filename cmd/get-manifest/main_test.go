package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildPlugin compiles the plugin once and returns its path.
func buildPlugin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "get-manifest")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building plugin: %v\n%s", err, out)
	}
	return bin
}

// fakeHelm writes a stub that stands in for the real helm binary. It records
// the arguments it was called with, so tests can assert on flag forwarding
// without needing a cluster.
func fakeHelm(t *testing.T, manifest string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	manifestFile := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestFile, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	bin = filepath.Join(dir, "helm")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > " + argsFile + "\ncat " + manifestFile + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func readFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "release.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type result struct {
	stdout, stderr string
	code           int
}

func runPlugin(t *testing.T, plugin, helmBin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(plugin, args...)
	// A clean environment keeps the developer's own HELM_NAMESPACE from
	// leaking into the assertions below.
	cmd.Env = append(os.Environ(), "HELM_BIN="+helmBin, "HELM_NAMESPACE=", "HELM_KUBECONTEXT=")
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()

	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running plugin: %v", err)
	}
	return result{out.String(), errb.String(), code}
}

// TestPassthrough is the central guarantee: with no filter, the plugin emits
// exactly what helm gave it.
func TestPassthrough(t *testing.T) {
	plugin := buildPlugin(t)
	fixture := readFixture(t)
	helm, _ := fakeHelm(t, fixture)

	got := runPlugin(t, plugin, helm, "demo")
	if got.code != exitOK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", got.code, exitOK, got.stderr)
	}
	if got.stdout != fixture {
		t.Errorf("output is not byte-identical to the stored manifest")
	}
}

func TestSourceFilter(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--source", "demo/templates/service.yaml")
	if got.code != exitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	want := "---\n# Source: demo/templates/service.yaml\n" +
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: demo\nspec:\n  ports:\n    - port: 80\n      targetPort: 8080\n"
	if got.stdout != want {
		t.Errorf("stdout = %q\nwant %q", got.stdout, want)
	}
}

// TestSubchartSourcesAreDistinct guards the collision seen in real releases,
// where a parent and a subchart both have templates/secret.yaml.
func TestSubchartSourcesAreDistinct(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	for source, wantName := range map[string]string{
		"demo/templates/secret.yaml":            "parent-secret",
		"demo/charts/sub/templates/secret.yaml": "sub-secret",
	} {
		got := runPlugin(t, plugin, helm, "demo", "--source", source, "--clean")
		if got.code != exitOK {
			t.Fatalf("%s: exit = %d (stderr: %s)", source, got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "name: "+wantName) {
			t.Errorf("--source %s selected the wrong document:\n%s", source, got.stdout)
		}
		if strings.Count(got.stdout, "kind: Secret") != 1 {
			t.Errorf("--source %s returned %d documents, want 1", source, strings.Count(got.stdout, "kind: Secret"))
		}
	}
}

func TestCleanSingleDocument(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--source", "demo/templates/configmap.yaml", "--clean")
	want := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo-config\ndata:\n  greeting: hello\n"
	if got.stdout != want {
		t.Errorf("stdout = %q\nwant %q", got.stdout, want)
	}
}

// TestCleanMultiDocument checks the separator rule: none leading, one between.
func TestCleanMultiDocument(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--source", "demo/templates/multi.yaml", "--clean")
	if got.code != exitOK {
		t.Fatalf("exit = %d, want 0", got.code)
	}
	if strings.HasPrefix(got.stdout, "---") {
		t.Errorf("clean output starts with a separator:\n%s", got.stdout)
	}
	if n := strings.Count(got.stdout, "\n---\n"); n != 1 {
		t.Errorf("got %d separators between documents, want 1:\n%s", n, got.stdout)
	}
	if strings.Contains(got.stdout, "# Source:") {
		t.Errorf("clean output still contains provenance:\n%s", got.stdout)
	}
	for _, name := range []string{"multi-a", "multi-b"} {
		if !strings.Contains(got.stdout, "name: "+name) {
			t.Errorf("clean output is missing %s:\n%s", name, got.stdout)
		}
	}
}

// TestCleanKeepsBlockScalarContent checks that --clean strips only Helm's own
// provenance, not a "# Source:" that is part of a resource's data.
func TestCleanKeepsBlockScalarContent(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--source", "demo/templates/tricky.yaml", "--clean")
	if got.code != exitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	if strings.HasPrefix(got.stdout, "# Source: demo") {
		t.Error("provenance comment was not stripped")
	}
	for _, want := range []string{
		"# Source: fake/templates/nope.yaml", // decoy inside a block scalar
		"    ---",                            // separator inside a block scalar
		"# A document separator inside",      // unrelated comment
	} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("clean output dropped %q:\n%s", want, got.stdout)
		}
	}
}

func TestListSources(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--list-sources")
	want := strings.Join([]string{
		"demo/templates/configmap.yaml",
		"demo/templates/service.yaml",
		"demo/templates/multi.yaml", // listed once despite two documents
		"demo/templates/tricky.yaml",
		"demo/charts/sub/templates/secret.yaml",
		"demo/templates/secret.yaml",
	}, "\n") + "\n"
	if got.stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", got.stdout, want)
	}
}

func TestSourceNotFound(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--source", "demo/templates/absent.yaml")
	if got.code != exitSourceNotFnd {
		t.Errorf("exit = %d, want %d", got.code, exitSourceNotFnd)
	}
	if got.stdout != "" {
		t.Errorf("stdout should stay empty on error, got %q", got.stdout)
	}
	if !strings.Contains(got.stderr, "source not found") {
		t.Errorf("stderr = %q, want a source-not-found message", got.stderr)
	}
}

// TestSourceNotFoundSuggests checks the near-match hint, which is what makes a
// mistyped bare filename recoverable.
func TestSourceNotFoundSuggests(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "demo", "--source", "secret.yaml")
	if got.code != exitSourceNotFnd {
		t.Fatalf("exit = %d, want %d", got.code, exitSourceNotFnd)
	}
	for _, want := range []string{
		"demo/templates/secret.yaml",
		"demo/charts/sub/templates/secret.yaml",
	} {
		if !strings.Contains(got.stderr, want) {
			t.Errorf("stderr should suggest %s:\n%s", want, got.stderr)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	for _, tt := range []struct {
		name string
		args []string
	}{
		{"no release", nil},
		{"unknown flag", []string{"demo", "--nope"}},
		{"missing flag value", []string{"demo", "--source"}},
		{"two releases", []string{"demo", "other"}},
		{"list-sources with source", []string{"demo", "--list-sources", "--source", "x"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := runPlugin(t, plugin, helm, tt.args...)
			if got.code != exitUsage {
				t.Errorf("exit = %d, want %d (stderr: %s)", got.code, exitUsage, got.stderr)
			}
		})
	}
}

func TestHelmFailure(t *testing.T) {
	plugin := buildPlugin(t)
	dir := t.TempDir()
	helm := filepath.Join(dir, "helm")
	if err := os.WriteFile(helm, []byte("#!/bin/sh\necho 'Error: release: not found' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := runPlugin(t, plugin, helm, "missing")
	if got.code != exitHelmFailed {
		t.Errorf("exit = %d, want %d", got.code, exitHelmFailed)
	}
	if !strings.Contains(got.stderr, "release: not found") {
		t.Errorf("helm's own error should reach stderr, got %q", got.stderr)
	}
}

// TestFlagForwarding checks that release-lookup flags reach helm, since these
// decide which stored manifest is read.
func TestFlagForwarding(t *testing.T) {
	plugin := buildPlugin(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"revision", []string{"demo", "--revision", "12"}, "get manifest demo --revision 12"},
		{"short namespace", []string{"demo", "-n", "prod"}, "get manifest demo --namespace prod"},
		{"long namespace", []string{"demo", "--namespace", "prod"}, "get manifest demo --namespace prod"},
		{"inline namespace", []string{"demo", "--namespace=prod"}, "get manifest demo --namespace prod"},
		{"kube context", []string{"demo", "--kube-context", "ctx"}, "get manifest demo --kube-context ctx"},
		{"combined", []string{"demo", "-n", "prod", "--revision", "3", "--kube-context", "ctx"},
			"get manifest demo --revision 3 --namespace prod --kube-context ctx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helm, argsFile := fakeHelm(t, readFixture(t))
			if got := runPlugin(t, plugin, helm, tt.args...); got.code != exitOK {
				t.Fatalf("exit = %d (stderr: %s)", got.code, got.stderr)
			}
			b, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("helm called with %q, want %q", b, tt.want)
			}
		})
	}
}

// TestNamespaceFromEnv checks that the plugin defaults to the namespace Helm
// exported, so it resolves the same release as `helm get manifest` would.
func TestNamespaceFromEnv(t *testing.T) {
	plugin := buildPlugin(t)
	helm, argsFile := fakeHelm(t, readFixture(t))

	cmd := exec.Command(plugin, "demo")
	cmd.Env = append(os.Environ(), "HELM_BIN="+helm, "HELM_NAMESPACE=from-env", "HELM_KUBECONTEXT=ctx-env")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running plugin: %v", err)
	}

	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "get manifest demo --namespace from-env --kube-context ctx-env"
	if string(b) != want {
		t.Errorf("helm called with %q, want %q", b, want)
	}
}

// TestExplicitNamespaceOverridesEnv checks precedence: the flag wins.
func TestExplicitNamespaceOverridesEnv(t *testing.T) {
	plugin := buildPlugin(t)
	helm, argsFile := fakeHelm(t, readFixture(t))

	cmd := exec.Command(plugin, "demo", "-n", "explicit")
	cmd.Env = append(os.Environ(), "HELM_BIN="+helm, "HELM_NAMESPACE=from-env", "HELM_KUBECONTEXT=")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running plugin: %v", err)
	}

	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := "get manifest demo --namespace explicit"; string(b) != want {
		t.Errorf("helm called with %q, want %q", b, want)
	}
}

func TestHelp(t *testing.T) {
	plugin := buildPlugin(t)
	helm, _ := fakeHelm(t, readFixture(t))

	got := runPlugin(t, plugin, helm, "--help")
	if got.code != exitOK {
		t.Errorf("exit = %d, want 0", got.code)
	}
	if !strings.Contains(got.stdout, "helm get-manifest") {
		t.Errorf("help should go to stdout, got %q", got.stdout)
	}
}
