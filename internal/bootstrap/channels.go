package bootstrap

import (
	"github.com/icloudbb/buildmax/internal/config"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	"github.com/icloudbb/buildmax/internal/infra/coordination"
	"github.com/icloudbb/buildmax/internal/infra/db"
	"github.com/icloudbb/buildmax/internal/infra/imchannel/telegram"
	servercoord "github.com/icloudbb/buildmax/internal/server/coordination"
	chansvc "github.com/icloudbb/buildmax/internal/service/channel"
)

// buildChannelGateway assembles the chat connectors server.yaml configures, or
// returns nil when it configures none. With a coordination backend, a lease
// keeps each connector receiving on one replica at a time.
func buildChannelGateway(sc config.ServerConfig, store *db.Store, elig eligibility.Checker, backend *coordination.Backend) *chansvc.Gateway {
	var connectors []corechannel.Connector
	if tg := sc.Channels.Telegram; tg.BotToken != "" {
		connectors = append(connectors, telegram.New(telegram.Config{Token: tg.BotToken, APIBaseURL: tg.APIBaseURL}))
	}
	var locker chansvc.Locker
	if backend != nil {
		locker = servercoord.NewConnectorLocker(backend)
	}
	return chansvc.New(chansvc.Config{
		Connectors:    connectors,
		Identities:    store,
		Conversations: store,
		Spaces:        store,
		Tasks:         store,
		Eligibility:   elig,
		PortalURL:     sc.PublicBaseURL,
		Locker:        locker,
	})
}
