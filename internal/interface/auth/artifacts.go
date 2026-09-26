package auth

import (
	"log/slog"

	"github.com/icloudbb/buildmax/internal/interface/client"
	"github.com/icloudbb/buildmax/internal/tool"
)

// ArtifactPublisherForSession returns this session's artifact capability, or
// nil when it has none.
//
// Being logged in is the whole precondition, and it is answered here at
// assembly time rather than discovered when the model calls the tool. A session
// with no server keeps its ordinary local output behaviour, and its agent is
// never offered a tool that could only fail — see
// docs/design/unified-artifacts.md section 7.1.
func ArtifactPublisherForSession() tool.ArtifactPublisher {
	info, err := Info()
	if err != nil {
		// A credentials file that cannot be read is not a reason to fail the
		// run; it means this session has no artifact capability, same as a
		// session that never logged in.
		slog.Debug("artifact capability unavailable", "err", err)
		return nil
	}
	if !info.LoggedIn || info.ServerURL == "" {
		return nil
	}
	return client.NewArtifactPublisher(info.ServerURL, "", TokenForServer, HTTPClient(info.ServerURL))
}
