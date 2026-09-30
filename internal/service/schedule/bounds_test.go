package schedule

import (
	"context"
	"strings"
	"testing"

	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	"github.com/icloudbb/buildmax/internal/mock"
)

// Name and input are bounded to their columns, so an over-long value is a clean
// invalid error rather than a write failure surfaced as a 500. The caps are
// checked before any store lookup, so a minimal wiring exercises them.
func TestCreate_FieldBounds(t *testing.T) {
	svc := &Service{Schedules: &mock.MockScheduleStore{}}
	ctx := context.Background()
	base := CreateCmd{
		SpaceID: "sp1", UserID: "u1", ExecutorKind: coreschedule.ExecutorAgent,
		ExecutorID: "a1", Input: "tick", CronExpr: "* * * * *", Timezone: "UTC",
	}
	long := base
	long.Name = strings.Repeat("n", maxScheduleNameRunes+1)
	if _, err := svc.Create(ctx, long); err != ErrNameTooLong {
		t.Errorf("over-long name err = %v, want ErrNameTooLong", err)
	}
	bigInput := base
	bigInput.Input = strings.Repeat("x", maxScheduleInputBytes+1)
	if _, err := svc.Create(ctx, bigInput); err != ErrInputTooLong {
		t.Errorf("over-long input err = %v, want ErrInputTooLong", err)
	}
}
