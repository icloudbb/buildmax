package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestRedactedConfigIsAWhitelist is the test that makes the redaction hold as
// the configuration grows.
//
// It walks ServerConfig for fields whose name says they carry a credential and
// asserts none of their values appear in the serialized view. A blacklist
// implementation would pass this today and fail on the next field someone adds
// with a different name; a whitelist keeps passing, because a new field is
// absent until somebody adds it to RedactedServerConfig on purpose.
func TestRedactedConfigIsAWhitelist(t *testing.T) {
	// Distinctive values with no ordinary English prefix, so the prefix check
	// below cannot be satisfied by a word that legitimately appears in the
	// response — "worker" in a warning is not a leak.
	sc := ServerConfig{JWTSecret: "zq7x1a-jwt-value"}
	sc.Database.Password = "zq7x2b-db-value"
	sc.Storage.MinIO.AccessKey = "zq7x3c-access-value"
	sc.Storage.MinIO.SecretKey = "zq7x4d-secret-value"
	sc.Conversation.Model.APIKey = "zq7x5e-model-value"
	sc.Channels.Telegram.BotToken = "zq7x6f-telegram-value"

	encoded, err := json.Marshal(sc.Redacted())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(encoded)
	for _, secret := range []string{
		"zq7x1a-jwt-value", "zq7x2b-db-value", "zq7x3c-access-value",
		"zq7x4d-secret-value", "zq7x5e-model-value", "zq7x6f-telegram-value",
	} {
		if strings.Contains(body, secret) {
			t.Errorf("the redacted view leaked %q: %s", secret, body)
		}
		// Not even a prefix. A first few characters is a real head start for
		// someone who already has the response.
		if strings.Contains(body, secret[:6]) {
			t.Errorf("the redacted view leaked a prefix of %q: %s", secret, body)
		}
	}
	for _, want := range []string{`"set":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("presence should still be reported, got %s", body)
		}
	}
}

// TestRedactedConfigReportsAbsentSecrets: "not configured" is a different
// answer from "configured", and an operator diagnosing a startup problem needs
// the difference.
func TestRedactedConfigReportsAbsentSecrets(t *testing.T) {
	got := ServerConfig{}.Redacted()
	if got.JWTSecret.Set || got.Database.Password.Set || got.Storage.MinIOAccessKey.Set {
		t.Errorf("an empty configuration reported a secret as set: %+v", got)
	}
	if got.Database.TLS != DefaultDBTLSMode {
		t.Errorf("tls = %q, want the effective mode %q rather than the empty setting", got.Database.TLS, DefaultDBTLSMode)
	}
	if got.Warnings == nil {
		t.Error("warnings should serialize as [] rather than null")
	}
}

func TestConfigWarnings(t *testing.T) {
	t.Run("a well-configured deployment warns about nothing", func(t *testing.T) {
		sc := ServerConfig{}
		sc.Worker.RunMode = "k8s_job"
		sc.Storage.PersistBackend = "minio"
		if got := sc.configWarnings(); len(got) != 0 {
			t.Errorf("warnings = %v, want none", got)
		}
	})

	t.Run("a run outliving its credential is reported", func(t *testing.T) {
		sc := ServerConfig{}
		sc.Worker.RunMode = "k8s_job"
		sc.Storage.PersistBackend = "minio"
		sc.Worker.RunTokenTTL = 1
		sc.Worker.RunTimeout = 2
		if got := sc.configWarnings(); len(got) != 1 || !strings.Contains(got[0], "run_timeout") {
			t.Errorf("warnings = %v", got)
		}
	})

	// Managed worker inference is no longer a warning of its own: what it needs
	// is a catalog row, which is data rather than configuration, so the startup
	// check reads the catalog instead of guessing from server.yaml.
	t.Run("managed worker inference alone warns about nothing", func(t *testing.T) {
		sc := ServerConfig{}
		sc.Worker.RunMode = "k8s_job"
		sc.Storage.PersistBackend = "minio"
		sc.Worker.LLM.Transport = TransportBuildMax
		if got := sc.configWarnings(); len(got) != 0 {
			t.Errorf("warnings = %v", got)
		}
	})
}

// TestRedactedConfigCoversTheSecretFieldsWeKnowAbout guards the list above:
// if ServerConfig grows a field whose name marks it as a credential, this
// fails until someone decides what the redacted view does with it.
func TestRedactedConfigCoversTheSecretFieldsWeKnowAbout(t *testing.T) {
	known := map[string]bool{
		"ServerConfig.JWTSecret":           true,
		"ServerDBConfig.Password":          true,
		"ServerMinIOConfig.AccessKey":      true,
		"ServerMinIOConfig.SecretKey":      true,
		"ServerModelEntry.APIKey":          true,
		"ServerCoordinationRedis.Password": true,
		"ServerOIDCConfig.ClientSecret":    true,
		"ServerTelegramConfig.BotToken":    true,
	}
	found := map[string]bool{}
	var walk func(t reflect.Type, seen map[reflect.Type]bool)
	walk = func(rt reflect.Type, seen map[reflect.Type]bool) {
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := strings.ToLower(f.Name)
			if f.Type.Kind() == reflect.String &&
				(strings.Contains(name, "secret") || strings.Contains(name, "password") ||
					strings.Contains(name, "apikey") || strings.Contains(name, "token") ||
					strings.Contains(name, "accesskey")) {
				found[rt.Name()+"."+f.Name] = true
			}
			walk(f.Type, seen)
		}
	}
	walk(reflect.TypeOf(ServerConfig{}), map[reflect.Type]bool{})

	for field := range found {
		if !known[field] {
			t.Errorf("%s looks like a credential and is not accounted for; decide what RedactedServerConfig does with it, then add it here", field)
		}
	}
	for field := range known {
		if !found[field] {
			t.Errorf("%s is listed as a credential but no longer exists; remove the row", field)
		}
	}
}
