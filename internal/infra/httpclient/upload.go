package httpclient

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

// UploadFile posts one local file as a multipart request and returns the
// response for the caller to decode and close.
//
// The body is streamed through a pipe rather than buffered: an artifact is
// whatever size the deployment allows, and a client that had to hold one in
// memory to send it would make the limit twice as expensive as it looks. The
// request can still be replayed — GetBody reopens the file — so a transport
// that renews a refused credential can send it again.
func UploadFile(ctx context.Context, client *http.Client, url, token, fieldName, path, filename string) (*http.Response, error) {
	// One boundary for every attempt: the Content-Type header names it, and a
	// replayed body must match the header it is sent under.
	form := multipart.NewWriter(io.Discard)
	newBody := func() (io.ReadCloser, error) {
		return multipartFileBody(path, fieldName, filename, form.Boundary())
	}
	body, err := newBody()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	req.GetBody = newBody
	req.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// multipartFileBody streams path as the single file part of a multipart body.
func multipartFileBody(path, fieldName, filename, boundary string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	if err := writer.SetBoundary(boundary); err != nil {
		_ = file.Close()
		return nil, err
	}
	go func() {
		defer func() { _ = file.Close() }()
		part, err := writer.CreateFormFile(fieldName, filename)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		// Closing the writer emits the trailing boundary; closing the pipe then
		// ends the request body. Both errors reach the reader, so a partial
		// upload fails the request rather than arriving truncated.
		_ = pw.CloseWithError(writer.Close())
	}()
	return pr, nil
}
