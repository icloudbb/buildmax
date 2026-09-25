package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// stubLinks is a ChannelLinks that knows one pending code, "GOODCODE", and
// records who confirmed it.
type stubLinks struct {
	links []corechannel.Identity
}

func (s *stubLinks) Platforms(context.Context) []corechannel.Info {
	return []corechannel.Info{{Platform: "telegram", Name: "Telegram", BotHandle: "@bot"}}
}

func (s *stubLinks) PreviewPairing(_ context.Context, code string) (*corechannel.Pairing, error) {
	if code != "GOODCODE" {
		return nil, corechannel.ErrPairingNotFound
	}
	return &corechannel.Pairing{Platform: "telegram", Handle: "@ada", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (s *stubLinks) ConfirmPairing(_ context.Context, userID, code string) (*corechannel.Identity, error) {
	if code != "GOODCODE" {
		return nil, corechannel.ErrPairingNotFound
	}
	link := corechannel.Identity{ID: "link1", UserID: userID, Platform: "telegram", Handle: "@ada", ExternalUserID: "4242"}
	s.links = append(s.links, link)
	return &link, nil
}

func (s *stubLinks) ListLinks(_ context.Context, userID string) ([]corechannel.Identity, error) {
	var out []corechannel.Identity
	for _, l := range s.links {
		if l.UserID == userID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *stubLinks) Unlink(_ context.Context, userID, id string) error {
	for i, l := range s.links {
		if l.ID == id && l.UserID == userID {
			s.links = append(s.links[:i], s.links[i+1:]...)
			return nil
		}
	}
	return apierr.ErrNotFound
}

func channelFixture(t *testing.T, links ChannelLinks) (*http.ServeMux, *mock.MockAuditStore) {
	t.Helper()
	store := &mock.MockAuditStore{}
	h := New(Config{JWTSecret: acctSecret, Users: &mock.MockUserStore{}, ChannelLinks: links, Audit: audit.NewRecorder(store)})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, store
}

func serve(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(acctMember, acctSecret))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestChannelLinkLifecycle(t *testing.T) {
	mux, store := channelFixture(t, &stubLinks{})

	rec := serve(mux, http.MethodGet, "/api/channel-link-pairings?code=GOODCODE", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"handle":"@ada"`) {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(mux, http.MethodGet, "/api/channel-link-pairings?code=WRONG", ""); rec.Code != http.StatusNotFound {
		t.Errorf("preview of a wrong code = %d, want 404", rec.Code)
	}

	rec = serve(mux, http.MethodPost, "/api/channel-links", `{"code":"GOODCODE"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "4242") {
		t.Errorf("the response exposes the platform account id: %s", rec.Body.String())
	}
	ev := firstEvent(t, store, coreaudit.ChannelLinkCreated)
	if ev.ActorID != acctMember || ev.TargetID != "link1" || ev.Detail != "telegram" {
		t.Errorf("channel_link.created event = %+v", ev)
	}

	rec = serve(mux, http.MethodGet, "/api/channel-links", "")
	var list listChannelLinksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Platforms) != 1 || list.Platforms[0].BotHandle != "@bot" || len(list.Links) != 1 || list.Links[0].ID != "link1" {
		t.Errorf("list = %+v", list)
	}

	if rec := serve(mux, http.MethodDelete, "/api/channel-links/link1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	firstEvent(t, store, coreaudit.ChannelLinkRemoved)
	if rec := serve(mux, http.MethodDelete, "/api/channel-links/link1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}
}

// With no chat platform configured the list still answers, so the Portal can
// say so, and the rest report the feature off.
func TestChannelLinksWithoutAPlatform(t *testing.T) {
	mux, _ := channelFixture(t, nil)
	rec := serve(mux, http.MethodGet, "/api/channel-links", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"platforms\":[],\"links\":[]}\n" {
		t.Errorf("list = %d %q", rec.Code, rec.Body.String())
	}
	if rec := serve(mux, http.MethodPost, "/api/channel-links", `{"code":"GOODCODE"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("confirm = %d, want 503", rec.Code)
	}
}
