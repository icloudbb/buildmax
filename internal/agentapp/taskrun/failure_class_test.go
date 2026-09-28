package taskrun

import (
	"errors"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// A provider failure is covered by infra/llm's IsProviderError test; here an
// ordinary error inside the run must not be filed as the model's.
func TestClassifyRunErrorFilesNonProviderFailuresAsTheRun(t *testing.T) {
	if got := classifyRunError(errors.New("max iterations exceeded")); got != coretask.FailureRun {
		t.Errorf("class = %q, want run", got)
	}
}
