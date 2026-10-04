package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/util"
)

// JobClient creates and deletes Kubernetes Jobs. Used by K8sJobRunner; can be implemented by a clientset wrapper or a fake in tests.
type JobClient interface {
	CreateJob(ctx context.Context, namespace string, job *batchv1.Job) error
	// DeleteJob removes the Job and its pods. A Job that is already gone is not
	// an error.
	DeleteJob(ctx context.Context, namespace, name string) error
}

// Volume and mount names for the worker pod's configuration.
const (
	homeVolumeName   = "buildmax-home"
	configVolumeName = "buildmax-config"
	caVolumeName     = "worker-api-ca"
	tmpVolumeName    = "tmp"
	serverConfigFile = "server.yaml"
)

// Worker pod labels. The production NetworkPolicy admits only pods carrying
// these to the worker listener, so they are a security selector rather than
// decoration, and a worker cannot relabel itself: it has no ServiceAccount
// token and never reaches the Kubernetes API. See
// docs/design/worker-api-network-boundary.md §7.2.
const (
	labelName       = "app.kubernetes.io/name"
	labelComponent  = "app.kubernetes.io/component"
	workerNameLabel = "buildmax-worker"
	workerComponent = "worker"
)

// WorkerPodLabels are stamped on every worker Job and its pod template. Exported
// so the manifest tests can assert the NetworkPolicy selects the same labels the
// runner actually sets.
func WorkerPodLabels() map[string]string {
	return map[string]string{
		labelName:      workerNameLabel,
		labelComponent: workerComponent,
	}
}

// PodConfig describes how a worker pod obtains its configuration.
//
// A worker reads server.yaml from BUILDMAX_HOME exactly as the server does, so
// the pod needs that file mounted and BUILDMAX_HOME pointing at it. Inherited
// environment variables cover credentials only; they are not a substitute for
// the file.
type PodConfig struct {
	// ConfigMapName holds a server.yaml key, mounted read-only into the pod.
	// Empty mounts no file — the worker then has defaults plus inherited env.
	ConfigMapName string
	// HomeDir becomes BUILDMAX_HOME in the pod and is where server.yaml lands.
	// Empty defaults to /buildmax.
	HomeDir string
	// CAConfigMapName holds the worker-api CA certificate, mounted read-only at
	// CAMountPath so the worker verifies the server listener. Empty mounts no CA
	// — the worker then uses the system roots, or plain HTTP in development.
	CAConfigMapName string
	// CAMountPath is where the CA lands in the pod; it equals worker.server_ca_file.
	// The ConfigMap key mounted there is that path's base name.
	CAMountPath string
	// Resources bounds the pod's CPU and memory. Every bound is required;
	// NewK8sJobRunner refuses a configuration that would leave one off the Job.
	Resources PodResources
	// FinishedJobTTL becomes the Job's ttlSecondsAfterFinished, and Kubernetes'
	// TTL controller deletes the Job and its pod once it passes; zero deletes
	// them on finish. Nothing reads a Job after it finishes — the run's result
	// arrives through the worker API — so the window exists only for
	// `kubectl logs`. It is short by default because a finished pod still holds
	// its emptyDir scratch disk on the node and its Job spec carries the run
	// token.
	FinishedJobTTL time.Duration
}

// PodResources holds Kubernetes quantity strings, e.g. "500m" or "1Gi". All
// four are required: a worker pod executes model-chosen shell commands, so an
// unbounded one is one runaway build away from starving every other pod on the
// node. Requirements reports why a deployment may not schedule workers.
type PodResources struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	// EphemeralStorageRequest and EphemeralStorageLimit bound the pod's local
	// scratch disk. They matter as much as memory for a checkpointing worker: the
	// materialized workspace, the compressed payload staged for upload, and tool
	// output all consume it, and an unbounded pod fills the node's disk instead
	// of being evicted. The limit is also the sizeLimit of each emptyDir.
	EphemeralStorageRequest string
	EphemeralStorageLimit   string
}

func (p PodConfig) homeDir() string {
	if p.HomeDir == "" {
		return "/buildmax"
	}
	return p.HomeDir
}

// Requirements converts the configured quantities into the bounds a worker
// container carries, or reports the first one a deployment got wrong.
//
// This used to drop an unparseable value with a warning, so that a typo in a
// limit would not stop a deployment from executing work. It should stop it: the
// result was an unbounded pod that looked configured, and a warning in a server
// log is not a bound. Rejecting the configuration is what makes the bound real.
func (p PodResources) Requirements() (corev1.ResourceRequirements, error) {
	cpuRequest, err := positiveQuantity("cpu_request", p.CPURequest)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	memoryRequest, err := positiveQuantity("memory_request", p.MemoryRequest)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	cpuLimit, err := positiveQuantity("cpu_limit", p.CPULimit)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	memoryLimit, err := positiveQuantity("memory_limit", p.MemoryLimit)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	ephemeralRequest, err := positiveQuantity("ephemeral_storage_request", p.EphemeralStorageRequest)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	ephemeralLimit, err := positiveQuantity("ephemeral_storage_limit", p.EphemeralStorageLimit)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	// Kubernetes rejects these at admission, which surfaces as every run
	// failing to schedule with no obvious cause. Naming the pair here costs one
	// comparison and answers the question the API server's error does not.
	if cpuLimit.Cmp(cpuRequest) < 0 {
		return corev1.ResourceRequirements{}, fmt.Errorf(
			"worker.k8s.resources: cpu_limit %q is below cpu_request %q", p.CPULimit, p.CPURequest)
	}
	if memoryLimit.Cmp(memoryRequest) < 0 {
		return corev1.ResourceRequirements{}, fmt.Errorf(
			"worker.k8s.resources: memory_limit %q is below memory_request %q", p.MemoryLimit, p.MemoryRequest)
	}
	if ephemeralLimit.Cmp(ephemeralRequest) < 0 {
		return corev1.ResourceRequirements{}, fmt.Errorf(
			"worker.k8s.resources: ephemeral_storage_limit %q is below ephemeral_storage_request %q", p.EphemeralStorageLimit, p.EphemeralStorageRequest)
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              cpuRequest,
			corev1.ResourceMemory:           memoryRequest,
			corev1.ResourceEphemeralStorage: ephemeralRequest,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:              cpuLimit,
			corev1.ResourceMemory:           memoryLimit,
			corev1.ResourceEphemeralStorage: ephemeralLimit,
		},
	}, nil
}

// positiveQuantity parses one bound. The error names the configuration key an
// operator edits, not the Go field, and shows the shape a value should have:
// the common mistake is a unit Kubernetes does not use, such as "4 gigabytes".
func positiveQuantity(field, value string) (resource.Quantity, error) {
	if strings.TrimSpace(value) == "" {
		return resource.Quantity{}, fmt.Errorf(
			"worker.k8s.resources.%s is required: a worker pod runs model-chosen commands and must be bounded", field)
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf(
			"worker.k8s.resources.%s = %q is not a Kubernetes quantity such as 500m, 2, 512Mi, or 4Gi: %w", field, value, err)
	}
	if q.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf(
			"worker.k8s.resources.%s = %q must be greater than zero", field, value)
	}
	return q, nil
}

// workerSeccompProfilePath is where deployment/buildmax-deploy.yaml's and
// deployment/production/buildmax.yaml's DaemonSet installs
// deployment/seccomp/worker-bwrap.json on every node, relative to the
// kubelet's seccomp root (<kubelet-root-dir>/seccomp, default
// /var/lib/kubelet/seccomp). Kubernetes' Localhost profile type takes this
// path, not file contents, so it has to match the DaemonSet's target exactly.
const workerSeccompProfilePath = "buildmax/worker-bwrap.json"

// podSecurityContext confines the worker pod.
//
// A worker executes model-chosen shell commands, so the pod is treated as
// running untrusted code even though the space that submitted the task is
// trusted: the prompt, the repository content, and the tool output that steer
// those commands are not. The containment this pod relies on is bwrap's own
// sandboxing of that worker's Bash commands, confined to the run's own
// workspace -- not the pod running non-root, which this pod does not do; see
// containerSecurityContext for why.
//
// The seccomp profile is Localhost, not RuntimeDefault: confirmed against a
// real pod carrying this exact security context, RuntimeDefault's own
// unshare/setns/mount/umount2/pivot_root/clone/clone3 rules are dropped
// entirely once capabilities are empty, which makes bubblewrap unable to
// create its own sandbox namespaces at all -- see
// deployment/seccomp/README.md for the full root-cause chain and how it was
// verified.
func (p PodConfig) podSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: util.Ptr(workerSeccompProfilePath),
		},
	}
}

// containerSecurityContext drops every Linux capability except SYS_ADMIN and
// NET_ADMIN, and runs root rather than non-root -- both against
// this package's own prior design, and both forced by the same finding: on a
// real Deployment smoke run, a non-root pod with SYS_ADMIN (or any other
// capability) added via Capabilities.Add never had it in its effective set at
// exec time, only its bounding set -- a capability a container runtime adds
// for a non-root container lands in the bounding set only, confirmed by
// isolating every other variable (seccomp, AppArmor, no-new-privileges, file
// capabilities on bwrap itself -- which bwrap explicitly refuses as a
// suspicious configuration) one at a time against a throwaway container. Root
// does not have this gap: an added capability is effective immediately. The
// read-only root filesystem, dropped capability set otherwise, and AppArmor
// override below are what still confine this pod; running as uid 0 is the
// one property this pod no longer has, in exchange for bwrap's own sandbox
// actually running instead of failing fail_if_unavailable's own closed
// refusal to start any task at all.
//
// AppArmorProfile is Unconfined for the same reason the seccomp profile above
// is Localhost rather than the default: this pod set no AppArmor field at
// all before this change, so a node applied its own default confinement
// (RuntimeDefault) automatically, and bwrap failed with "Creating new
// namespace failed" -- AppArmor's own apparmor_restrict_unprivileged_userns
// hardening denying the unprivileged unshare(CLONE_NEWUSER) bwrap needs on a
// host carrying it.
//
// NET_ADMIN is what bwrap needs to bring up loopback in the network namespace
// it gives a command, whose only way out is then the sandbox proxy; without
// it a command that ignores HTTP_PROXY reaches the network directly. bwrap
// drops every capability before the command runs (--cap-drop ALL), so neither
// SYS_ADMIN nor NET_ADMIN reaches model-chosen code.
//
// tmpVolumeName restores the one writable path shell commands assume, since
// the root filesystem stays read-only even as root.
func containerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: util.Ptr(false),
		ReadOnlyRootFilesystem:   util.Ptr(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  []corev1.Capability{"SYS_ADMIN", "NET_ADMIN"},
		},
		AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined},
	}
}

// K8sJobRunner creates a Kubernetes Job per task. Run returns immediately after Job create.
type K8sJobRunner struct {
	namespace string
	image     string
	env       []corev1.EnvVar
	pod       PodConfig
	resources corev1.ResourceRequirements
	client    JobClient
}

// NewK8sJobRunner returns a runner that creates a Job in the given namespace with
// the given image, inherited env, and configuration source for the worker pod.
//
// The resource bounds are resolved here rather than per run, so a deployment
// configured to produce an unbounded worker fails at startup, where an operator
// is reading errors, instead of at the first run that needed the bound.
func NewK8sJobRunner(namespace, image string, env []corev1.EnvVar, pod PodConfig, client JobClient) (*K8sJobRunner, error) {
	resources, err := pod.Resources.Requirements()
	if err != nil {
		return nil, err
	}
	if pod.FinishedJobTTL < 0 {
		return nil, fmt.Errorf("worker.k8s.finished_job_ttl = %s must not be negative", pod.FinishedJobTTL)
	}
	return &K8sJobRunner{namespace: namespace, image: image, env: env, pod: pod, resources: resources, client: client}, nil
}

// podEnv returns the inherited environment with BUILDMAX_HOME set to the pod's
// home directory, plus this run's gateway credential when one was minted. Any
// inherited BUILDMAX_HOME is dropped: it names a path on the server, which does
// not exist in the worker pod.
//
// The run token is a plain value in the Job spec, so anyone who can read Job
// objects in this namespace can read it. That is a known limit of the first
// version — the token names one run, expires, and stops working when the run
// does. See docs/design/worker-run-token.md.
func (r *K8sJobRunner) podEnv(runToken string) []corev1.EnvVar {
	out := make([]corev1.EnvVar, 0, len(r.env)+2)
	for _, e := range r.env {
		if e.Name == config.EnvKeyBuildmaxHome {
			continue
		}
		out = append(out, e)
	}
	out = append(out, corev1.EnvVar{Name: config.EnvKeyBuildmaxHome, Value: r.pod.homeDir()})
	if runToken != "" {
		out = append(out, corev1.EnvVar{Name: config.EnvKeyBuildmaxRunToken, Value: runToken})
	}
	return out
}

// emptyDirSizeLimit returns the pod's ephemeral-storage limit as a per-volume
// cap, or nil when none is configured — a runner built without resource bounds,
// as some tests do. The returned pointer is a copy, safe to hand to a volume.
func emptyDirSizeLimit(res corev1.ResourceRequirements) *resource.Quantity {
	q, ok := res.Limits[corev1.ResourceEphemeralStorage]
	if !ok {
		return nil
	}
	c := q.DeepCopy()
	return &c
}

// podVolumes returns the writable home volume plus, when configured, the
// ConfigMap carrying server.yaml.
func (r *K8sJobRunner) podVolumes() ([]corev1.Volume, []corev1.VolumeMount) {
	home := r.pod.homeDir()
	// Cap each scratch volume at the pod's ephemeral-storage limit. Writing past
	// a sizeLimit evicts the pod cleanly, so a runaway workspace or a checkpoint
	// payload too large to stage is stopped at the volume rather than filling the
	// node's disk. The container limit bounds the sum; this bounds each volume.
	sizeLimit := emptyDirSizeLimit(r.resources)
	volumes := []corev1.Volume{{
		Name:         homeVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: sizeLimit}},
	}, {
		// The root filesystem is read-only, and shell commands assume a
		// writable /tmp. Without this the worker runs but ordinary tooling
		// fails in ways that look like the tool is broken.
		Name:         tmpVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: sizeLimit}},
	}}
	mounts := []corev1.VolumeMount{
		{Name: homeVolumeName, MountPath: home},
		{Name: tmpVolumeName, MountPath: "/tmp"},
	}

	// The worker-api CA, when the deployment mounts one. Independent of the
	// server.yaml ConfigMap: a worker over HTTPS needs the CA even where the rest
	// of its configuration arrives by inherited environment.
	if r.pod.CAConfigMapName != "" && r.pod.CAMountPath != "" {
		key := filepath.Base(r.pod.CAMountPath)
		volumes = append(volumes, corev1.Volume{
			Name: caVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: r.pod.CAConfigMapName},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      caVolumeName,
			MountPath: r.pod.CAMountPath,
			SubPath:   key,
			ReadOnly:  true,
		})
	}

	if r.pod.ConfigMapName == "" {
		return volumes, mounts
	}
	volumes = append(volumes, corev1.Volume{
		Name: configVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: r.pod.ConfigMapName},
			},
		},
	})
	// subPath mounts the single file, leaving the rest of BUILDMAX_HOME writable
	// for sessions, logs, and traces.
	mounts = append(mounts, corev1.VolumeMount{
		Name:      configVolumeName,
		MountPath: home + "/" + serverConfigFile,
		SubPath:   serverConfigFile,
		ReadOnly:  true,
	})
	return volumes, mounts
}

// Run creates a Job for the task run; on success returns ("k8s_job", &jobName, &createdAt, nil). On failure returns error.
//
// runToken is this run's managed-gateway credential, or "" when the deployment
// mints none.
func (r *K8sJobRunner) Run(ctx context.Context, run coretask.Run, runToken string) (workerType string, k8sJobName *string, k8sJobCreatedAt *time.Time, err error) {
	jobName := workerJobNameForTaskRun(run.ID)
	now := metav1.Now()
	createdAt := now.UTC()

	volumes, mounts := r.podVolumes()
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: r.namespace, Labels: WorkerPodLabels()},
		Spec: batchv1.JobSpec{
			BackoffLimit:            util.Ptr(int32(3)),
			TTLSecondsAfterFinished: util.Ptr(int32(r.pod.FinishedJobTTL / time.Second)),
			Template: corev1.PodTemplateSpec{
				// The pod labels are what the NetworkPolicy selects, so they must
				// be on the template, not only the Job.
				ObjectMeta: metav1.ObjectMeta{Labels: WorkerPodLabels()},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					// A drained or evicted worker stops its agent, uploads what
					// the run produced, and reports the outcome. That window is
					// bounded by taskrun.interruptReportTimeout; this is the
					// deadline it has to fit inside, set explicitly rather than
					// inherited so the two are visible together. See
					// docs/design/graceful-shutdown.md §6.3.
					TerminationGracePeriodSeconds: util.Ptr(int64(30)),
					Volumes:                       volumes,
					// A worker never calls the Kubernetes API, so a mounted
					// service-account token is only a credential for
					// model-chosen commands to find.
					AutomountServiceAccountToken: util.Ptr(false),
					// Kubernetes otherwise writes the address of every Service
					// in the namespace into the container's environment, handing
					// model-chosen commands a map of the database, object store,
					// and cache. A worker reaches only BUILDMAX_SERVER_URL.
					EnableServiceLinks: util.Ptr(false),
					SecurityContext:    r.pod.podSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            "worker",
							Image:           r.image,
							Command:         []string{"buildmax-worker"},
							Args:            []string{"--task-run-id", run.ID},
							Env:             r.podEnv(runToken),
							VolumeMounts:    mounts,
							SecurityContext: containerSecurityContext(),
							Resources:       r.resources,
						},
					},
				},
			},
		},
	}

	if err := r.client.CreateJob(ctx, r.namespace, job); err != nil {
		componentLog().Warn("failed to create k8s Job", "task_run_id", run.ID, "space_id", run.SpaceID, "job_name", jobName, "err", err)
		return "", nil, nil, err
	}
	componentLog().Info("created k8s Job", "task_run_id", run.ID, "space_id", run.SpaceID, "job_name", jobName, "namespace", r.namespace)
	return "k8s_job", &jobName, &createdAt, nil
}

// DeleteWorkerJob removes a run's Job and its pods, for a run the server has
// already ended without its worker.
func (r *K8sJobRunner) DeleteWorkerJob(ctx context.Context, jobName string) error {
	return r.client.DeleteJob(ctx, r.namespace, jobName)
}

// jobClientImpl implements JobClient using a Kubernetes clientset.
type jobClientImpl struct {
	clientset kubernetes.Interface
}

func (c *jobClientImpl) CreateJob(ctx context.Context, namespace string, job *batchv1.Job) error {
	_, err := c.clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	return err
}

// DeleteJob deletes in the background: the Job's own default is to orphan its
// pods, which would leave the worker running.
func (c *jobClientImpl) DeleteJob(ctx context.Context, namespace, name string) error {
	policy := metav1.DeletePropagationBackground
	err := c.clientset.BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// WorkerEnvFromEnviron returns the environment a worker Job pod is given: the
// BUILDMAX_* variables from this process that a worker actually reads.
//
// The server holds credentials a worker has no use for — the JWT signing
// secret, the database password — and forwarding them would hand every
// model-chosen command the ability to mint tokens for any user and read the
// whole database. config.WorkerNeedsEnv decides; see its comment for how to
// extend the set.
//
// A credential the worker does read is not copied as a value: the pod gets a
// reference to credentialSecret, the Secret the server's own copy came from,
// under a key named like the variable. A value in the Job spec would be
// readable by anyone who can read Jobs or Pods in the namespace. An empty
// credentialSecret is refused when there is a credential to pass.
//
// managedLLM says whether task runs reach models through the server, which
// withholds the provider credential as well.
func WorkerEnvFromEnviron(managedLLM bool, credentialSecret string) ([]corev1.EnvVar, error) {
	env := os.Environ()
	var out []corev1.EnvVar
	for _, e := range env {
		if !strings.HasPrefix(e, "BUILDMAX_") {
			continue
		}
		idx := strings.IndexByte(e, '=')
		if idx <= 0 {
			continue
		}
		name := e[:idx]
		if !config.WorkerNeedsEnv(name, managedLLM) {
			continue
		}
		if !config.IsCredentialEnv(name) {
			out = append(out, corev1.EnvVar{Name: name, Value: e[idx+1:]})
			continue
		}
		if credentialSecret == "" {
			return nil, fmt.Errorf("worker.k8s.credential_secret is empty, but the server holds %s for its workers: name the Secret worker pods read it from", name)
		}
		out = append(out, corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: credentialSecret},
				Key:                  name,
			},
		}})
	}
	return out, nil
}

// BuildK8sJobClient builds rest config (in-cluster or kubeconfig), creates a clientset, and returns a JobClient.
// For use when worker.run_mode is k8s_job in server.yaml. Returns an error if not in cluster and no usable kubeconfig.
func BuildK8sJobClient() (JobClient, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &jobClientImpl{clientset: clientset}, nil
}

// Identity belongs in an attr, not in every message string.
func componentLog() *slog.Logger { return slog.With("component", "k8s_runner") }
