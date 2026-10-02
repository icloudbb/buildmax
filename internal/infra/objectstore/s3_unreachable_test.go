package objectstore

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

func adapterFor(t *testing.T, endpoint string) S3Client {
	t.Helper()
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("k", "s", "")),
		awsconfig.WithRetryMaxAttempts(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	return NewS3ClientAdapter(s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	}))
}

// A store that never answers is a 503 the caller may retry, not a 500 that
// reads as a server bug. Every operation classifies it the same way.
func TestUnreachableStorageIsUnavailable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // nothing listens: every dial is refused
	a := adapterFor(t, "http://"+addr)
	ctx := context.Background()
	_, getErr := a.GetObject(ctx, "b", "k")
	_, _, streamErr := a.GetObjectStream(ctx, "b", "k")
	_, existsErr := a.ObjectExists(ctx, "b", "k")
	_, listErr := a.ListObjectKeys(ctx, "b", "p/")
	for name, err := range map[string]error{"GetObject": getErr, "GetObjectStream": streamErr, "ObjectExists": existsErr, "ListObjectKeys": listErr,
		"DeleteObject": a.DeleteObject(ctx, "b", "k")} {
		if !errors.Is(err, ErrStorageUnavailable) {
			t.Errorf("%s = %v, want ErrStorageUnavailable", name, err)
		}
		if kind, _ := apierr.KindOf(err); kind != apierr.KindUnavailable {
			t.Errorf("%s kind = %q, want unavailable", name, kind)
		}
	}
}

// A store that answers with an error is not unavailable: a refused key is a
// configuration problem the operator must see as such.
func TestAnsweredStorageErrorIsNotUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>denied</Message></Error>`))
	}))
	defer srv.Close()
	_, err := adapterFor(t, srv.URL).GetObject(context.Background(), "b", "k")
	if err == nil || errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("GetObject = %v, want the store's own refusal, not ErrStorageUnavailable", err)
	}
}
