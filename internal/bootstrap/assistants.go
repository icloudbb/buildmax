package bootstrap

import (
	"fmt"

	"github.com/icloudbb/buildmax/internal/config"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/infra/db"
	"github.com/icloudbb/buildmax/internal/infra/imchannel/telegram"
	assistantsvc "github.com/icloudbb/buildmax/internal/service/assistant"
	"github.com/icloudbb/buildmax/internal/service/audit"
	chansvc "github.com/icloudbb/buildmax/internal/service/channel"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
)

// buildAssistants assembles Space Assistants over the chat Gateway. Their bots
// share the system bot's Telegram API base URL, which only tests change.
func buildAssistants(sc config.ServerConfig, store *db.Store, files assistantsvc.SpaceFiles, channels *chansvc.Gateway, gw *llmgateway.Service) *assistantsvc.Service {
	if channels == nil {
		return nil
	}
	apiBase := sc.Channels.Telegram.APIBaseURL
	svc := &assistantsvc.Service{
		Store:           store,
		Spaces:          store,
		Users:           store,
		ServiceAccounts: &spacesvc.Service{Spaces: store, Users: store, ServiceAccounts: store},
		Agents:          store,
		Workflows:       store,
		Artifacts:       store,
		Secrets:         store,
		SpaceFiles:      files,
		Audit:           audit.NewRecorder(store),
		Bots: &assistantsvc.Reconciler{
			Store:   store,
			Gateway: channels,
			Connect: func(platform, token string) (corechannel.Connector, error) {
				if platform != corechannel.PlatformTelegram {
					return nil, fmt.Errorf("unsupported chat platform %q", platform)
				}
				return telegram.New(telegram.Config{Token: token, APIBaseURL: apiBase}), nil
			},
		},
	}
	if gw != nil {
		svc.Models = gw
	}
	return svc
}
