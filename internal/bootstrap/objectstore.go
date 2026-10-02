// Package bootstrap wires process startup dependencies.
package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/service/plugin"
	"path/filepath"

	"github.com/icloudbb/buildmax/internal/agentapp/taskrun"
	"github.com/icloudbb/buildmax/internal/config"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BuildS3Client creates an S3-compatible client. Use when either provider is minio.
//
// Two shapes are supported, and the endpoint is what distinguishes them: a
// deployment naming an endpoint is talking to a store it runs or a vendor's
// S3-compatible service, while one that names none is talking to AWS S3 and
// wants the SDK's own regional endpoint resolution.
//
// Credentials follow the same principle. Static keys are used when configured;
// leaving them empty falls through to the SDK's default chain, which is how a
// cluster reaches a bucket through IRSA, workload identity, or an instance
// profile instead of a long-lived key the deployment has to store and rotate.
func BuildS3Client(ctx context.Context, cfg config.WorkspaceStorageConfig) (blob.S3Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" || cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)))
	}
	opts = append(opts, awsconfig.WithHTTPClient(storageHTTPClient()))
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = usePathStyle(cfg)
	})
	return blob.NewS3ClientAdapter(client), nil
}

// Bounds on reaching object storage. The SDK's client has no connect timeout of
// its own, so an endpoint that drops packets held each attempt for the kernel's
// TCP timeout and a download for minutes. These bound the waits, not the
// transfer: the response-header wait starts after the request body is sent, so
// a large upload is not cut short.
//
// A dial timeout alone does not cover an outage that starts while the pool
// holds idle connections: a request on one is written into a link that drops
// it and waited out the response-header timeout, attempt after attempt, which
// measured 70s on DigitalOcean. storageUnackedTimeout closes such a connection
// once data sent on it goes unacknowledged that long. A slow but live store
// still acknowledges what it receives, so this is not a limit on how long it
// may take to answer. With the SDK's three attempts an unreachable store fails
// a call in about half a minute, as ErrStorageUnavailable.
const (
	storageDialTimeout           = 5 * time.Second
	storageTLSHandshakeTimeout   = 10 * time.Second
	storageResponseHeaderTimeout = 30 * time.Second
	storageUnackedTimeout        = 10 * time.Second
)

func storageHTTPClient() *awshttp.BuildableClient {
	return awshttp.NewBuildableClient().
		WithDialerOptions(func(d *net.Dialer) {
			d.Timeout = storageDialTimeout
			d.Control = boundUnackedData
		}).
		WithTransportOptions(func(t *http.Transport) {
			t.TLSHandshakeTimeout = storageTLSHandshakeTimeout
			t.ResponseHeaderTimeout = storageResponseHeaderTimeout
		})
}

// usePathStyle decides bucket addressing.
//
// An explicit setting wins. Otherwise a configured endpoint means an
// S3-compatible store, which needs bucket-in-path; no endpoint means AWS S3,
// where virtual-host addressing is the supported form for buckets created
// since 2020.
func usePathStyle(cfg config.WorkspaceStorageConfig) bool {
	if cfg.PathStyle != nil {
		return *cfg.PathStyle
	}
	return cfg.Endpoint != ""
}

// BuildPersistStorage returns the configured persist storage implementation.
func BuildPersistStorage(cfg config.WorkspaceStorageConfig, persistRoot func(spaceID string) string, runGlobalDir func(spaceID, taskID, taskRunID string) string, s3Client blob.S3Client) (blob.PersistStorage, error) {
	switch cfg.PersistProvider {
	case config.ProviderMinIO:
		if s3Client == nil {
			return nil, fmt.Errorf("persist storage is minio but S3 client is nil")
		}
		return blob.NewS3PersistStorage(s3Client, cfg.Bucket, cfg.Prefix), nil
	default:
		return blob.NewLocalFSPersistStorage(persistRoot, runGlobalDir), nil
	}
}

// BuildArtifactStorage returns the configured storage for artifact content.
// artifactDir is (spaceID, artifactID) -> directory for the local-filesystem
// backend; the S3 backend derives its own key and ignores it.
func BuildArtifactStorage(cfg config.WorkspaceStorageConfig, artifactDir func(spaceID, artifactID string) string, s3Client blob.S3Client) (artifactsvc.ContentStore, error) {
	switch cfg.ArtifactProvider {
	case config.ProviderMinIO:
		if s3Client == nil {
			return nil, fmt.Errorf("artifact storage is minio but S3 client is nil")
		}
		return blob.NewS3ArtifactStorage(s3Client, cfg.Bucket, cfg.Prefix), nil
	default:
		return blob.NewLocalFSArtifactStorage(artifactDir), nil
	}
}

// BuildCheckpointStore returns the checkpoint payload store the server owns. It
// uses the same backend as artifacts: object storage when one is configured,
// otherwise the local filesystem under checkpointRoot. The worker's own store
// (which writes the bytes) must address the same backend and prefix. The full
// store surface is returned so the server can hand the finalizer its read view
// and the orphan sweep its maintenance view from one instance.
func BuildCheckpointStore(cfg config.WorkspaceStorageConfig, checkpointRoot string, s3Client blob.S3Client) (blob.CheckpointStore, error) {
	switch cfg.ArtifactProvider {
	case config.ProviderMinIO:
		if s3Client == nil {
			return nil, fmt.Errorf("checkpoint storage is minio but S3 client is nil")
		}
		return blob.NewS3CheckpointStore(s3Client, cfg.Bucket, cfg.Prefix), nil
	default:
		return blob.NewLocalFSCheckpointStore(checkpointRoot), nil
	}
}

// BuildWorkerCheckpointStore returns the store a worker writes captured
// checkpoint payloads to. It addresses the same backend and prefix as
// BuildCheckpointPayloadStore, which the server reads to verify those bytes and
// derive their key — the two must agree or a finalize would not find what a
// worker uploaded.
func BuildWorkerCheckpointStore(cfg config.WorkspaceStorageConfig, checkpointRoot string, s3Client blob.S3Client) (taskrun.CheckpointPayloadStore, error) {
	switch cfg.ArtifactProvider {
	case config.ProviderMinIO:
		if s3Client == nil {
			return nil, fmt.Errorf("checkpoint storage is minio but S3 client is nil")
		}
		return blob.NewS3CheckpointStore(s3Client, cfg.Bucket, cfg.Prefix), nil
	default:
		return blob.NewLocalFSCheckpointStore(checkpointRoot), nil
	}
}

// PluginPackagesDirName is where a deployment with no object store keeps
// published packages. It is dot-prefixed so it cannot be mistaken for a space's
// workspace directory, which is what every other entry under workspaces_dir is.
const PluginPackagesDirName = ".marketplace"

// BuildPluginPackageStorage returns storage for published plugin packages and
// the key prefix to build keys with.
//
// There is no provider setting of its own. A deployment that has an object
// store keeps packages in it, and one that does not keeps them on the server's
// disk — the same decision it already made for everything else it stores, and
// one fewer knob to set inconsistently.
//
// Packages are kept apart from space artifacts on purpose: a catalog record that
// vanished with a space's retention window could no longer explain an
// installation still sitting on somebody's machine.
func BuildPluginPackageStorage(cfg config.WorkspaceStorageConfig, workspacesDir string, s3Client blob.S3Client) (plugin.PackageStore, string) {
	if s3Client != nil {
		return blob.NewS3PluginPackageStorage(s3Client, cfg.Bucket), cfg.Prefix
	}
	return blob.NewLocalFSPluginPackageStorage(filepath.Join(workspacesDir, PluginPackagesDirName)), ""
}
