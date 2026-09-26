package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/infra/db"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/spacerecovery"
)

// The operator-side break-glass for a shared Space whose owners can all no
// longer sign in. It is the counterpart to `buildmax admin` against a running
// server: the same disabled-owner-only recovery, reachable from the machine that
// holds the database credentials when the public Server or IdP is unavailable.
//
// It is deliberately narrow. It cannot transfer a healthy Space, touch a
// personal Space, or create a membership — the same preconditions the service
// enforces for the Admin API. See docs/design/system-administration.md §8.4.

// SpaceCommandUsage is the help text for `buildmax-server space`.
const SpaceCommandUsage = `Usage: buildmax-server space <command> [args]

Commands:
  recover-owner <space_id> <successor_email>
        Promote an enabled member to owner of a shared space when every
        recorded owner is disabled. Refuses a personal space, a space whose
        owner can still sign in, and a successor who is not already a member.

This is break glass: routine ownership transfer is done by a space owner in the
Portal or with ` + "`buildmax`" + `. Use this only when no owner can sign in.
`

// RunSpaceCommand executes `buildmax-server space ...`. args excludes the
// "space" word itself.
func RunSpaceCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, SpaceCommandUsage)
		return errors.New("space: a command is required")
	}
	switch args[0] {
	case "recover-owner":
		return runSpaceRecoverOwner(ctx, args[1:], out)
	case "help", "-h", "--help":
		fmt.Fprint(out, SpaceCommandUsage)
		return nil
	default:
		fmt.Fprint(out, SpaceCommandUsage)
		return fmt.Errorf("space: unknown command %q", args[0])
	}
}

func runSpaceRecoverOwner(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 {
		fmt.Fprint(out, SpaceCommandUsage)
		return errors.New("recover-owner: a space id and a successor email are required")
	}
	spaceID := strings.TrimSpace(args[0])
	successorEmail := strings.TrimSpace(args[1])

	sc, err := config.LoadServerConfig()
	if err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	dsn := sc.Database.DSN()
	if dsn == "" {
		return fmt.Errorf("database is not configured in %s", config.ServerConfigPath())
	}
	store, err := db.New(ctx, dsn, db.Options{AllowNewerSchema: sc.Database.AllowNewerSchema})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	successor, err := store.UserByEmail(ctx, successorEmail)
	if err != nil {
		return fmt.Errorf("look up successor: %w", err)
	}
	if successor == nil {
		return fmt.Errorf("%s has no account; the successor must already be an enabled member of the space", successorEmail)
	}

	svc := &spacerecovery.Service{Spaces: store, Users: store}
	demotedOwner, err := svc.RecoverOwnership(ctx, spacerecovery.RecoverCmd{
		SpaceID:     spaceID,
		SuccessorID: successor.ID,
	})
	if err != nil {
		return fmt.Errorf("recover ownership: %w", err)
	}
	// The operator, not a user, is the actor here: this runs from a shell on the
	// machine that already holds the database credentials, so inventing a user id
	// would put an unverified name in the record.
	audit.NewRecorder(store).Record(ctx, coreaudit.Event{
		ActorType:  coreaudit.ActorSystem,
		ActorID:    coreaudit.ActorOperator,
		SpaceID:    spaceID,
		Action:     coreaudit.SpaceOwnershipRecovered,
		TargetType: "user",
		TargetID:   successor.ID,
		Detail:     demotedOwner,
	})
	fmt.Fprintf(out, "Recovered ownership of %s: %s (%s) is now owner; the disabled owner was demoted to admin.\n",
		spaceID, successor.Email, successor.ID)
	return nil
}
