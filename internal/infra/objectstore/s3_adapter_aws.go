package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// ErrStorageUnavailable is a call object storage never answered: the
// connection failed or timed out before any response. It is a 503 to the
// caller rather than a 500, because the server is fine and a retry may work.
var ErrStorageUnavailable = apierr.New(apierr.KindUnavailable, "object storage is unavailable; try again shortly")

// unreachable marks an error that carries no S3 response as storage being
// unavailable, keeping the original in the chain. An error with a response
// (403, 500, a missing key) is the store answering, and passes through. The SDK
// wraps a failed send in a ResponseError too, with status 0, so the status is
// what tells them apart.
func unreachable(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	var resp *smithyhttp.ResponseError
	if errors.As(err, &resp) && resp.HTTPStatusCode() != 0 {
		return err
	}
	return fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
}

// ObjectInfo is one listed object's key and last-modified time. The orphan sweep
// needs the time to leave recently written blobs alone, so listing that dropped
// it would force a HEAD per object.
type ObjectInfo struct {
	Key     string
	ModTime time.Time
}

// S3Client is a minimal S3-compatible client used by persist and artifact storage.
// Implementations can wrap AWS SDK v2 or MinIO; tests can provide a fake.
type S3Client interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader) error
	GetObject(ctx context.Context, bucket, key string) ([]byte, error)
	// GetObjectStream opens an object without reading it into memory, and
	// reports its size. It exists for objects too large to hold: a plugin
	// package is bounded, but bounded at tens of megabytes per request, and
	// artifact content is whatever a deployment's limit allows.
	GetObjectStream(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error)
	// DeleteObject removes one object. A key that is not there is not an error:
	// the caller is a delete path that has to be safe to retry.
	DeleteObject(ctx context.Context, bucket, key string) error
	// ListObjectKeys returns object keys under the given prefix (keys include the prefix).
	// Prefix should end with "/" for directory-style listing.
	ListObjectKeys(ctx context.Context, bucket, prefix string) ([]string, error)
	// ListObjects returns each object under the prefix with its last-modified
	// time, for callers that must reason about object age. Keys include the
	// prefix; prefix should end with "/" for directory-style listing.
	ListObjects(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)
	// ObjectExists reports whether an object is present.
	ObjectExists(ctx context.Context, bucket, key string) (bool, error)
}

// s3ClientAdapter adapts *s3.Client to S3Client.
type s3ClientAdapter struct {
	client   *s3.Client
	uploader *transfermanager.Client
}

// NewS3ClientAdapter returns an S3Client that uses the given AWS S3 client.
func NewS3ClientAdapter(client *s3.Client) S3Client {
	return &s3ClientAdapter{client: client, uploader: transfermanager.New(client)}
}

// PutObject uploads through the transfer manager, not a bare PutObject.
//
// A bare PutObject cannot send a body that is neither seekable nor of known
// length: over plain HTTP the SDK can compute neither the signing payload hash
// (which needs to rewind the stream) nor a trailing checksum (which needs TLS),
// and it fails the request outright with "unseekable stream is not supported".
// Artifact uploads are exactly that body — a multipart part streamed straight
// from the request. The transfer manager reads the stream into bounded parts
// and sends each as its own signable request, so an unseekable source works
// without spooling the whole object to the server's disk. A seekable body
// still uploads correctly through the same path.
func (a *s3ClientAdapter) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	_, err := a.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	return unreachable(ctx, err)
}

func (a *s3ClientAdapter) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, apierr.ErrNotFound
		}
		return nil, unreachable(ctx, err)
	}
	defer func() { _ = out.Body.Close() }()
	return io.ReadAll(out.Body)
}

func (a *s3ClientAdapter) GetObjectStream(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error) {
	out, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, 0, apierr.ErrNotFound
		}
		return nil, 0, unreachable(ctx, err)
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}

func (a *s3ClientAdapter) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := a.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var nsk *types.NoSuchKey
	var nf *types.NotFound
	if errors.As(err, &nsk) || errors.As(err, &nf) {
		return false, nil
	}
	return false, unreachable(ctx, err)
}

// DeleteObject reports success for a key that is not there. S3 already behaves
// this way, and the caller is a delete path that must be safe to retry.
func (a *s3ClientAdapter) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := a.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil
		}
		return unreachable(ctx, err)
	}
	return nil
}

func (a *s3ClientAdapter) ListObjectKeys(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", unreachable(ctx, err))
		}
		for _, o := range page.Contents {
			if o.Key != nil {
				keys = append(keys, *o.Key)
			}
		}
	}
	return keys, nil
}

func (a *s3ClientAdapter) ListObjects(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	var out []ObjectInfo
	paginator := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", unreachable(ctx, err))
		}
		for _, o := range page.Contents {
			if o.Key == nil {
				continue
			}
			var mod time.Time
			if o.LastModified != nil {
				mod = *o.LastModified
			}
			out = append(out, ObjectInfo{Key: *o.Key, ModTime: mod})
		}
	}
	return out, nil
}
