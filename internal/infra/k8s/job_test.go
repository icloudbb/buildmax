package k8s

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// fakeJobCreator records the last created Job for tests.
type fakeJobCreator struct {
	lastJob *batchv1.Job
}

func (f *fakeJobCreator) CreateJob(ctx context.Context, namespace string, job *batchv1.Job) error {
	f.lastJob = job.DeepCopy()
	return nil
}

// newTestRunner builds a runner for a test whose subject is something other
// than the resource bounds, supplying valid ones when the case did not set its
// own. Every bound is required, so a test that says nothing about resources
// still has to carry a set; TestJobPodResources owns the cases that do not.
func newTestRunner(t *testing.T, namespace, image string, env []corev1.EnvVar, pod PodConfig, client JobCreator) *K8sJobRunner {
	t.Helper()
	if pod.Resources == (PodResources{}) {
		pod.Resources = PodResources{
			CPURequest: "250m", CPULimit: "1", MemoryRequest: "512Mi", MemoryLimit: "1Gi",
			EphemeralStorageRequest: "256Mi", EphemeralStorageLimit: "2Gi",
		}
	}
	r, err := NewK8sJobRunner(namespace, image, env, pod, client)
	if err != nil {
		t.Fatalf("NewK8sJobRunner: %v", err)
	}
	return r
}

func TestK8sJobRunner_Run_SetsJobNamePattern(t *testing.T) {
	fake := &fakeJobCreator{}
	runner := newTestRunner(t, "buildmax", "buildmax:local", []corev1.EnvVar{}, PodConfig{}, fake)
	run := coretask.Run{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TaskID: "chat1", Status: "SCHEDULED"}

	workerType, k8sName, k8sAt, err := runner.Run(context.Background(), run, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if workerType != "k8s_job" {
		t.Errorf("workerType = %q, want k8s_job", workerType)
	}
	if k8sName == nil || *k8sName == "" {
		t.Error("k8sJobName should be set")
	}
	if k8sAt == nil || k8sAt.IsZero() {
		t.Error("k8sJobCreatedAt should be set")
	}
	pattern := regexp.MustCompile(`^buildmax-worker-[a-z0-9-]+-\d+$`)
	if !pattern.MatchString(*k8sName) {
		t.Errorf("job name %q does not match pattern buildmax-worker-<id>-<timestamp>", *k8sName)
	}
	if fake.lastJob == nil || fake.lastJob.Name != *k8sName {
		t.Errorf("fake Job name = %v, want %q", fake.lastJob.Name, *k8sName)
	}
}

// TestK8sJobRunner_MountsServerConfig covers the contract the k8s deployment
// path depends on: a worker pod gets server.yaml mounted from the ConfigMap and
// BUILDMAX_HOME pointing at the directory it lands in. Without both, the worker
// falls back to built-in defaults and cannot reach the database or storage.
func TestK8sJobRunner_MountsServerConfig(t *testing.T) {
	fake := &fakeJobCreator{}
	inherited := []corev1.EnvVar{
		{Name: config.EnvKeyBuildmaxMinIOAccessKey, Value: "minio-key"},
		{Name: config.EnvKeyBuildmaxHome, Value: "/server-side-path"},
	}
	runner := newTestRunner(t, "buildmax", "buildmax:local", inherited,
		PodConfig{ConfigMapName: "buildmax-config", HomeDir: "/buildmax"}, fake)

	if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "r_1"}, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	container := fake.lastJob.Spec.Template.Spec.Containers[0]

	// BUILDMAX_HOME must be the pod's path exactly once — the server's own value
	// names a directory that does not exist in the worker pod.
	var homeValues []string
	for _, e := range container.Env {
		if e.Name == config.EnvKeyBuildmaxHome {
			homeValues = append(homeValues, e.Value)
		}
	}
	if len(homeValues) != 1 || homeValues[0] != "/buildmax" {
		t.Errorf("%s in pod = %v, want exactly [/buildmax]", config.EnvKeyBuildmaxHome, homeValues)
	}

	// Credentials inherited from the server must survive.
	var sawToken bool
	for _, e := range container.Env {
		if e.Name == config.EnvKeyBuildmaxMinIOAccessKey && e.Value == "minio-key" {
			sawToken = true
		}
	}
	if !sawToken {
		t.Errorf("inherited %s was not propagated to the worker pod", config.EnvKeyBuildmaxMinIOAccessKey)
	}

	// server.yaml must be mounted by subPath so the rest of BUILDMAX_HOME stays
	// writable for sessions, logs, and traces.
	var configMount *corev1.VolumeMount
	for i, m := range container.VolumeMounts {
		if m.MountPath == "/buildmax/server.yaml" {
			configMount = &container.VolumeMounts[i]
		}
	}
	if configMount == nil {
		t.Fatalf("server.yaml is not mounted; mounts = %+v", container.VolumeMounts)
	}
	if configMount.SubPath != "server.yaml" {
		t.Errorf("config mount SubPath = %q, want server.yaml", configMount.SubPath)
	}

	var sawConfigMap, sawHomeVolume bool
	for _, v := range fake.lastJob.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && v.ConfigMap.Name == "buildmax-config" {
			sawConfigMap = true
		}
		if v.EmptyDir != nil {
			sawHomeVolume = true
		}
	}
	if !sawConfigMap {
		t.Error("worker pod has no buildmax-config ConfigMap volume")
	}
	if !sawHomeVolume {
		t.Error("worker pod has no writable volume for BUILDMAX_HOME")
	}
}

// TestK8sJobRunner_NoConfigMap keeps the degenerate case explicit: with no
// ConfigMap configured the pod still gets a writable home, just no config file.
func TestK8sJobRunner_NoConfigMap(t *testing.T) {
	fake := &fakeJobCreator{}
	runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{}, fake)
	if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "r_2"}, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, v := range fake.lastJob.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil {
			t.Errorf("unexpected ConfigMap volume %q when none is configured", v.ConfigMap.Name)
		}
	}
	container := fake.lastJob.Spec.Template.Spec.Containers[0]
	paths := make(map[string]bool, len(container.VolumeMounts))
	for _, m := range container.VolumeMounts {
		paths[m.MountPath] = true
	}
	// The default home plus the writable /tmp a read-only root filesystem
	// requires. Nothing config-shaped.
	if !paths["/buildmax"] || !paths["/tmp"] || len(paths) != 2 {
		t.Errorf("mounts = %+v, want the default home at /buildmax and a writable /tmp", container.VolumeMounts)
	}
}

// TestWorkerEnvFromEnviron_WithholdsServerOnlyCredentials asserts the Job pod
// gets what a worker reads and nothing else. The server process holds the JWT
// signing secret and the database password; forwarding them would give every
// model-chosen command in the pod the ability to mint tokens for any user and
// read the whole database.
func TestWorkerEnvFromEnviron_WithholdsServerOnlyCredentials(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxServerURL, "https://server.example")
	t.Setenv(config.EnvKeyBuildmaxMinIOSecretKey, "minio-secret")
	t.Setenv(config.EnvKeyBuildmaxMinIOAccessKey, "minio-key")
	t.Setenv(config.EnvKeyBuildmaxJWTSecret, "jwt-secret")
	t.Setenv(config.EnvKeyBuildmaxDatabasePassword, "db-secret")
	t.Setenv("BUILDMAX_ADDED_LATER", "unknown")
	t.Setenv("PATH_LIKE_NON_BUILDMAX", "ignored")

	got := WorkerEnvFromEnviron(false)
	byName := make(map[string]string, len(got))
	for _, e := range got {
		byName[e.Name] = e.Value
	}

	for _, name := range []string{config.EnvKeyBuildmaxJWTSecret, config.EnvKeyBuildmaxDatabasePassword, "BUILDMAX_ADDED_LATER"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s must not be forwarded to a worker pod", name)
		}
	}
	for _, name := range []string{config.EnvKeyBuildmaxServerURL, config.EnvKeyBuildmaxMinIOSecretKey, config.EnvKeyBuildmaxMinIOAccessKey} {
		if _, ok := byName[name]; !ok {
			t.Errorf("%s is read by a worker but was not forwarded", name)
		}
	}
	if _, ok := byName["PATH_LIKE_NON_BUILDMAX"]; ok {
		t.Error("only BUILDMAX_ variables belong in the pod env built here")
	}
}

// TestJobPodIsConfined asserts the containment a worker pod is created with.
// A worker runs model-chosen shell commands, so each of these is load-bearing
// rather than hygiene, and each has silently regressed elsewhere before.
func TestJobPodIsConfined(t *testing.T) {
	fake := &fakeJobCreator{}
	r := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{ConfigMapName: "buildmax-config"}, fake)
	if _, _, _, err := r.Run(context.Background(), coretask.Run{ID: "run-1"}, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.lastJob == nil {
		t.Fatal("no job created")
	}
	spec := fake.lastJob.Spec.Template.Spec

	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Error("a worker never calls the Kubernetes API; its token must not be mounted")
	}
	psc := spec.SecurityContext
	if psc == nil {
		t.Fatal("pod security context missing")
	}
	// Root, not non-root: see containerSecurityContext's own comment for why
	// -- a non-root pod's added capabilities never land in its effective set
	// on a real cluster, confirmed against a real Deployment smoke run.
	if psc.RunAsNonRoot != nil && *psc.RunAsNonRoot {
		t.Error("worker pods run as root now; RunAsNonRoot must not be set true")
	}
	// Not RuntimeDefault: confirmed against a real pod carrying this exact
	// security context that RuntimeDefault drops bubblewrap's required
	// syscalls once capabilities are empty. See deployment/seccomp/README.md.
	if psc.SeccompProfile == nil || psc.SeccompProfile.Type != corev1.SeccompProfileTypeLocalhost {
		t.Errorf("seccomp profile type = %v, want Localhost", psc.SeccompProfile)
	}
	if psc.SeccompProfile == nil || psc.SeccompProfile.LocalhostProfile == nil || *psc.SeccompProfile.LocalhostProfile != workerSeccompProfilePath {
		t.Errorf("seccomp localhost profile = %v, want %q", psc.SeccompProfile, workerSeccompProfilePath)
	}

	csc := spec.Containers[0].SecurityContext
	if csc == nil {
		t.Fatal("container security context missing")
	}
	if csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation {
		t.Error("privilege escalation must be denied")
	}
	if csc.ReadOnlyRootFilesystem == nil || !*csc.ReadOnlyRootFilesystem {
		t.Error("the root filesystem must be read-only")
	}
	if len(csc.Capabilities.Drop) != 1 || csc.Capabilities.Drop[0] != "ALL" {
		t.Errorf("capabilities drop = %v, want [ALL]", csc.Capabilities.Drop)
	}
	// SYS_ADMIN added back: bwrap needs it to build its sandbox, and running
	// root (above) is what makes an added capability actually effective.
	wantAdd := []corev1.Capability{"SYS_ADMIN"}
	if !slices.Equal(csc.Capabilities.Add, wantAdd) {
		t.Errorf("capabilities add = %v, want %v", csc.Capabilities.Add, wantAdd)
	}
	// Unconfined rather than a node's default AppArmor confinement: a
	// Deployment smoke run on a host carrying Ubuntu's
	// apparmor_restrict_unprivileged_userns hardening reproduced bubblewrap's
	// "Creating new namespace failed" even with the seccomp profile above in
	// place, the same way RuntimeDefault seccomp broke it before that fix.
	if csc.AppArmorProfile == nil || csc.AppArmorProfile.Type != corev1.AppArmorProfileTypeUnconfined {
		t.Errorf("apparmor profile type = %v, want Unconfined", csc.AppArmorProfile)
	}

	// A read-only root filesystem without a writable /tmp breaks ordinary
	// tooling in ways that look like the tool is broken, so the two ship
	// together or not at all.
	var hasTmp bool
	for _, m := range spec.Containers[0].VolumeMounts {
		if m.MountPath == "/tmp" {
			hasTmp = true
		}
	}
	if !hasTmp {
		t.Error("a read-only root filesystem needs a writable /tmp")
	}
}

// TestJobPodResources covers what a deployment gets for each way of
// configuring the bounds. The failing cases matter more than the passing one: a
// worker pod runs model-chosen shell commands, so every path that would create
// one without a bound has to end in a refusal an operator can read.
func TestJobPodResources(t *testing.T) {
	t.Run("configured bounds reach the pod", func(t *testing.T) {
		fake := &fakeJobCreator{}
		r := newTestRunner(t, "buildmax", "img", nil, PodConfig{
			Resources: PodResources{
				CPURequest: "250m", CPULimit: "2", MemoryRequest: "512Mi", MemoryLimit: "4Gi",
				EphemeralStorageRequest: "1Gi", EphemeralStorageLimit: "8Gi",
			},
		}, fake)
		if _, _, _, err := r.Run(context.Background(), coretask.Run{ID: "run-1"}, ""); err != nil {
			t.Fatalf("Run: %v", err)
		}
		res := fake.lastJob.Spec.Template.Spec.Containers[0].Resources
		if got := res.Limits.Memory().String(); got != "4Gi" {
			t.Errorf("memory limit = %s, want 4Gi", got)
		}
		if got := res.Limits.Cpu().String(); got != "2" {
			t.Errorf("cpu limit = %s, want 2", got)
		}
		if got := res.Requests.Memory().String(); got != "512Mi" {
			t.Errorf("memory request = %s, want 512Mi", got)
		}
		if got := res.Requests.Cpu().String(); got != "250m" {
			t.Errorf("cpu request = %s, want 250m", got)
		}
		if got := res.Limits.StorageEphemeral().String(); got != "8Gi" {
			t.Errorf("ephemeral-storage limit = %s, want 8Gi", got)
		}
		if got := res.Requests.StorageEphemeral().String(); got != "1Gi" {
			t.Errorf("ephemeral-storage request = %s, want 1Gi", got)
		}
		// The limit is also the sizeLimit of each writable emptyDir, so a runaway
		// workspace is evicted at the volume rather than filling the node.
		vols := fake.lastJob.Spec.Template.Spec.Volumes
		emptyDirs := 0
		for _, v := range vols {
			if v.EmptyDir == nil {
				continue
			}
			emptyDirs++
			if v.EmptyDir.SizeLimit == nil || v.EmptyDir.SizeLimit.String() != "8Gi" {
				t.Errorf("volume %q sizeLimit = %v, want 8Gi", v.Name, v.EmptyDir.SizeLimit)
			}
		}
		if emptyDirs != 2 {
			t.Errorf("expected 2 emptyDir volumes (home, tmp), got %d", emptyDirs)
		}
	})

	// Each of these once produced a Job. Unset produced one with no bounds at
	// all; the others produced one missing exactly the bound that was typed
	// wrong, with a warning in the server log as the only trace.
	rejected := []struct {
		name      string
		resources PodResources
		wantField string
	}{
		{
			name:      "nothing configured",
			resources: PodResources{},
			wantField: "cpu_request",
		},
		{
			name:      "one bound left out",
			resources: PodResources{CPURequest: "250m", CPULimit: "2", MemoryRequest: "512Mi"},
			wantField: "memory_limit",
		},
		{
			name:      "a unit Kubernetes does not use",
			resources: PodResources{CPURequest: "250m", CPULimit: "2", MemoryRequest: "512Mi", MemoryLimit: "4 gigabytes"},
			wantField: "memory_limit",
		},
		{
			name:      "a bound of zero",
			resources: PodResources{CPURequest: "250m", CPULimit: "0", MemoryRequest: "512Mi", MemoryLimit: "4Gi"},
			wantField: "cpu_limit",
		},
		{
			name:      "a limit below its request",
			resources: PodResources{CPURequest: "250m", CPULimit: "2", MemoryRequest: "4Gi", MemoryLimit: "512Mi", EphemeralStorageRequest: "1Gi", EphemeralStorageLimit: "8Gi"},
			wantField: "memory_limit",
		},
		{
			name:      "no ephemeral-storage bound",
			resources: PodResources{CPURequest: "250m", CPULimit: "2", MemoryRequest: "512Mi", MemoryLimit: "4Gi"},
			wantField: "ephemeral_storage_request",
		},
		{
			name:      "ephemeral-storage limit below its request",
			resources: PodResources{CPURequest: "250m", CPULimit: "2", MemoryRequest: "512Mi", MemoryLimit: "4Gi", EphemeralStorageRequest: "8Gi", EphemeralStorageLimit: "1Gi"},
			wantField: "ephemeral_storage_limit",
		},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeJobCreator{}
			r, err := NewK8sJobRunner("buildmax", "img", nil, PodConfig{Resources: tc.resources}, fake)
			if err == nil {
				t.Fatalf("the configuration was accepted; a worker Job would run unbounded")
			}
			if r != nil {
				t.Error("a rejected configuration must not yield a runner")
			}
			// The message is what an operator has to act on, so it names the
			// key they edit rather than the Go field or the type.
			if !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("error %q does not name %s", err, tc.wantField)
			}
			if fake.lastJob != nil {
				t.Error("no Job may be created for a rejected configuration")
			}
		})
	}
}

// TestK8sJobRunner_CarriesRunToken covers how a run's gateway credential reaches
// the pod. It is per run, so it cannot be part of the inherited environment the
// runner was built with, and a deployment that mints none must not gain an empty
// variable that looks like a configured one.
func TestK8sJobRunner_CarriesRunToken(t *testing.T) {
	envValues := func(job *batchv1.Job, name string) []string {
		var out []string
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			if e.Name == name {
				out = append(out, e.Value)
			}
		}
		return out
	}

	t.Run("minted", func(t *testing.T) {
		fake := &fakeJobCreator{}
		runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{HomeDir: "/buildmax"}, fake)
		if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "r_1"}, "signed-token"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := envValues(fake.lastJob, config.EnvKeyBuildmaxRunToken); len(got) != 1 || got[0] != "signed-token" {
			t.Errorf("%s in pod = %v, want exactly [signed-token]", config.EnvKeyBuildmaxRunToken, got)
		}
	})

	t.Run("none", func(t *testing.T) {
		fake := &fakeJobCreator{}
		runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{HomeDir: "/buildmax"}, fake)
		if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "r_2"}, ""); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := envValues(fake.lastJob, config.EnvKeyBuildmaxRunToken); len(got) != 0 {
			t.Errorf("%s in pod = %v, want it absent", config.EnvKeyBuildmaxRunToken, got)
		}
	})
}

// TestWorkerEnvFromEnviron_DropsInheritedRunToken records that the only run
// token a worker can find is the one minted for its own run. A stale value in
// the server's environment must not travel into a pod, where it would name some
// other run.
func TestWorkerEnvFromEnviron_DropsInheritedRunToken(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxRunToken, "some-other-runs-token")
	t.Setenv(config.EnvKeyBuildmaxMinIOAccessKey, "kept")

	env := WorkerEnvFromEnviron(false)
	var sawInherited bool
	for _, e := range env {
		if e.Name == config.EnvKeyBuildmaxRunToken {
			t.Errorf("%s was inherited from the server environment", config.EnvKeyBuildmaxRunToken)
		}
		if e.Name == config.EnvKeyBuildmaxMinIOAccessKey {
			sawInherited = true
		}
	}
	if !sawInherited {
		t.Errorf("%s was not propagated; the filter dropped too much", config.EnvKeyBuildmaxMinIOAccessKey)
	}
}

// TestWorkerEnvFromEnviron_ManagedPodHasNoProviderKey covers the pod half of
// removing the upstream credential. A managed run reaches models through the
// server, so a provider key in the pod would be a secret with no purpose sitting
// where model-chosen commands run.
func TestWorkerEnvFromEnviron_ManagedPodHasNoProviderKey(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxConversationAPIKey, "provider-key")
	t.Setenv(config.EnvKeyBuildmaxMinIOSecretKey, "minio-secret")

	names := func(env []corev1.EnvVar) map[string]string {
		out := make(map[string]string, len(env))
		for _, e := range env {
			out[e.Name] = e.Value
		}
		return out
	}

	managed := names(WorkerEnvFromEnviron(true))
	if _, ok := managed[config.EnvKeyBuildmaxConversationAPIKey]; ok {
		t.Errorf("%s reached a managed worker pod", config.EnvKeyBuildmaxConversationAPIKey)
	}
	if _, ok := managed[config.EnvKeyBuildmaxMinIOSecretKey]; !ok {
		t.Error("managed filtering dropped the storage credential, which every run still needs")
	}

	if _, ok := names(WorkerEnvFromEnviron(false))[config.EnvKeyBuildmaxConversationAPIKey]; !ok {
		t.Errorf("%s was withheld from a direct worker pod", config.EnvKeyBuildmaxConversationAPIKey)
	}
}

// TestK8sJobRunner_LabelsWorkerPod covers the selector the production
// NetworkPolicy relies on: the labels must be on the pod template, not only the
// Job, or the policy would admit nothing. See
// docs/design/worker-api-network-boundary.md §7.2.
func TestK8sJobRunner_LabelsWorkerPod(t *testing.T) {
	fake := &fakeJobCreator{}
	runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{}, fake)
	if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "run1", Status: "SCHEDULED"}, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := WorkerPodLabels()
	for _, got := range []map[string]string{fake.lastJob.Labels, fake.lastJob.Spec.Template.Labels} {
		for k, v := range want {
			if got[k] != v {
				t.Errorf("label %q = %q, want %q (labels = %v)", k, got[k], v, got)
			}
		}
	}
	if fake.lastJob.Spec.Template.Labels["app.kubernetes.io/name"] != "buildmax-worker" {
		t.Error("the pod template must carry the worker name label the NetworkPolicy selects")
	}
}

// TestK8sJobRunner_FinishedJobExpires guards against finished worker Jobs
// accumulating: nothing else deletes them. Zero must still be set on the Job —
// it means delete on finish, while an unset field means never.
func TestK8sJobRunner_FinishedJobExpires(t *testing.T) {
	for _, tc := range []struct {
		ttl  time.Duration
		want int32
	}{
		{5 * time.Minute, 300},
		{0, 0},
	} {
		fake := &fakeJobCreator{}
		runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{FinishedJobTTL: tc.ttl}, fake)
		if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "run1", Status: "SCHEDULED"}, ""); err != nil {
			t.Fatalf("Run: %v", err)
		}
		got := fake.lastJob.Spec.TTLSecondsAfterFinished
		if got == nil || *got != tc.want {
			t.Errorf("FinishedJobTTL %s: TTLSecondsAfterFinished = %v, want %d", tc.ttl, got, tc.want)
		}
	}
}

func TestNewK8sJobRunner_RejectsNegativeFinishedJobTTL(t *testing.T) {
	pod := PodConfig{
		FinishedJobTTL: -time.Second,
		Resources: PodResources{
			CPURequest: "250m", CPULimit: "1", MemoryRequest: "512Mi", MemoryLimit: "1Gi",
			EphemeralStorageRequest: "256Mi", EphemeralStorageLimit: "2Gi",
		},
	}
	_, err := NewK8sJobRunner("buildmax", "buildmax:local", nil, pod, &fakeJobCreator{})
	if err == nil || !strings.Contains(err.Error(), "worker.k8s.finished_job_ttl") {
		t.Fatalf("err = %v, want a refusal naming worker.k8s.finished_job_ttl", err)
	}
}

// TestK8sJobRunner_MountsWorkerAPICA covers the CA delivery a worker over HTTPS
// depends on: the configured ConfigMap is mounted read-only at server_ca_file.
func TestK8sJobRunner_MountsWorkerAPICA(t *testing.T) {
	fake := &fakeJobCreator{}
	runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{
		CAConfigMapName: "buildmax-worker-api-ca",
		CAMountPath:     "/buildmax/tls/worker-api-ca.crt",
	}, fake)
	if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "run1", Status: "SCHEDULED"}, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spec := fake.lastJob.Spec.Template.Spec

	var caMount *corev1.VolumeMount
	for i, m := range spec.Containers[0].VolumeMounts {
		if m.MountPath == "/buildmax/tls/worker-api-ca.crt" {
			caMount = &spec.Containers[0].VolumeMounts[i]
		}
	}
	if caMount == nil {
		t.Fatalf("CA is not mounted; mounts = %+v", spec.Containers[0].VolumeMounts)
	}
	if !caMount.ReadOnly {
		t.Error("the CA mount must be read-only")
	}
	if caMount.SubPath != "worker-api-ca.crt" {
		t.Errorf("CA SubPath = %q, want the file's base name", caMount.SubPath)
	}

	var caVol *corev1.Volume
	for i, v := range spec.Volumes {
		if v.Name == caMount.Name {
			caVol = &spec.Volumes[i]
		}
	}
	if caVol == nil || caVol.ConfigMap == nil || caVol.ConfigMap.Name != "buildmax-worker-api-ca" {
		t.Errorf("CA volume does not come from the configured ConfigMap: %+v", caVol)
	}
}

// TestK8sJobRunner_NoCAWithoutConfig keeps the mount opt-in: a deployment that
// configures none (plain HTTP development) gets no CA volume.
func TestK8sJobRunner_NoCAWithoutConfig(t *testing.T) {
	fake := &fakeJobCreator{}
	runner := newTestRunner(t, "buildmax", "buildmax:local", nil, PodConfig{}, fake)
	if _, _, _, err := runner.Run(context.Background(), coretask.Run{ID: "run1", Status: "SCHEDULED"}, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, v := range fake.lastJob.Spec.Template.Spec.Volumes {
		if v.Name == caVolumeName {
			t.Error("a CA volume was mounted with no ca_config_map configured")
		}
	}
}
