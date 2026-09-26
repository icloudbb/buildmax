package main

import (
	"archive/tar"
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestDiffFingerprints(t *testing.T) {
	before := map[string]string{
		"run/r1/status":       "SUCCEEDED",
		"run/r1/trace_sha256": "aaa",
		"artifact/a1/sha256":  "bbb",
	}
	after := map[string]string{
		"run/r1/status":       "SUCCEEDED",
		"run/r1/trace_sha256": "ccc",
		"audit/e9":            "user.login",
	}
	got := diffFingerprints(before, after)
	want := fingerprintDiff{
		Missing: []string{"artifact/a1/sha256 = bbb"},
		Changed: []string{"run/r1/trace_sha256: aaa -> ccc"},
		Added:   []string{"audit/e9 = user.login"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff = %+v, want %+v", got, want)
	}
	if !got.lost() {
		t.Fatal("a missing and a changed entry must count as loss")
	}
}

// Rows written by the restored deployment are new, not lost.
func TestDiffFingerprintsAddedOnlyIsNotLoss(t *testing.T) {
	got := diffFingerprints(map[string]string{"k": "v"}, map[string]string{"k": "v", "new": "x"})
	if got.lost() {
		t.Fatalf("an added entry alone reported loss: %+v", got)
	}
	if len(got.Added) != 1 {
		t.Fatalf("added = %v, want the one new entry", got.Added)
	}
}

func TestParseRowCounts(t *testing.T) {
	got, err := parseRowCounts("artifact\t3\ntask_run\t12\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"rows/artifact": "3", "rows/task_run": "12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
	if _, err := parseRowCounts("no tab here"); err == nil {
		t.Fatal("a malformed line was accepted")
	}
}

func TestBucketManifest(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(h *tar.Header, body string) {
		t.Helper()
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(&tar.Header{Name: "bmstore/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	write(&tar.Header{Name: "bmstore/workspaces/sp/home/a.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 5}, "hello")
	write(&tar.Header{Name: "./bmstore/workspaces/sp/artifacts/x/content", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0}, "")
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := bucketManifest(bytes.NewReader(buf.Bytes()), "bmstore")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"object/workspaces/sp/home/a.txt":          "5 bytes, sha256 " + sha256Hex("hello"),
		"object/workspaces/sp/artifacts/x/content": "0 bytes, sha256 " + sha256Hex(""),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %v, want %v", got, want)
	}

	var outside bytes.Buffer
	tw = tar.NewWriter(&outside)
	if err := tw.WriteHeader(&tar.Header{Name: "elsewhere/x", Typeflag: tar.TypeReg, Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	if _, err := bucketManifest(bytes.NewReader(outside.Bytes()), "bmstore"); err == nil {
		t.Fatal("an entry outside the bucket root was accepted")
	}
}

func TestWithoutManifestDocument(t *testing.T) {
	manifest := `# header
---
apiVersion: v1
kind: Namespace
metadata:
  name: buildmax
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: buildmax-config
data:
  server.yaml: |
    port: 5678
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: other
`
	got, err := withoutManifestDocument(manifest, "ConfigMap", "buildmax-config")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "buildmax-config") || strings.Contains(got, "port: 5678") {
		t.Fatalf("the ConfigMap survived:\n%s", got)
	}
	if !strings.Contains(got, "kind: Namespace") || !strings.Contains(got, "name: other") {
		t.Fatalf("other documents were dropped:\n%s", got)
	}
	if _, err := withoutManifestDocument(manifest, "ConfigMap", "missing"); err == nil {
		t.Fatal("a manifest without the document was accepted")
	}
}

// The committed manifest must keep exactly the ConfigMap the restore replaces.
func TestDeployManifestHasOneServerConfigMap(t *testing.T) {
	t.Chdir("../..")
	if _, err := manifestImage("deployment/kind/minio-init.yaml"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("deployment/buildmax-deploy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withoutManifestDocument(string(data), "ConfigMap", "buildmax-config"); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryConfigProblems(t *testing.T) {
	t.Setenv("BUILDMAX_TELEGRAM_BOT_TOKEN", "")
	if got := recoveryConfigProblems("port: 5678\nchannels:\n  telegram:\n    bot_token: \"\"\n"); len(got) != 0 {
		t.Fatalf("an empty token was refused: %v", got)
	}
	if got := recoveryConfigProblems("channels:\n  telegram:\n    bot_token: 123:abc\n"); len(got) != 1 {
		t.Fatalf("a configured bot token was not refused: %v", got)
	}
	t.Setenv("BUILDMAX_TELEGRAM_BOT_TOKEN", "123:abc")
	if got := recoveryConfigProblems("port: 5678\n"); len(got) != 1 {
		t.Fatalf("an environment bot token was not refused: %v", got)
	}
}
