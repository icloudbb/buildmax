import { Button } from "@buildmax/gui"
import type { IssueOutput } from "../../lib/types"
import { artifactContentUrl } from "../artifacts"
import { downloadAuthenticated } from "../../lib/download"
import { navigate } from "../../router"

interface OutputCardProps {
  output: IssueOutput
  token?: string | null
  onOpenConversation?: (conversationId: string) => void
  onOpenRun?: (workflowRunId: string) => void
  onOpenTrace?: (taskRunId: string) => void
}

function formatTimestamp(rfc3339: string): string {
  return new Date(rfc3339).toLocaleString()
}

// An issue output is an artifact a run published: a file reached by its own id
// and taken away, not previewed inline. The run is where it came from.
export function OutputCard({
  output,
  token,
  onOpenConversation,
  onOpenRun,
  onOpenTrace,
}: OutputCardProps) {
  const { source } = output
  return (
    <article className="issue-outputs__card">
      <header className="issue-outputs__card-head">
        <div>
          <h3 className="issue-outputs__card-title">
            <button
              type="button"
              className="issue-outputs__card-link"
              onClick={() => navigate({ name: "artifact", artifactId: output.artifactId })}
            >
              {output.title}
            </button>
          </h3>
          <div className="page-activity__meta">
            {output.filename ? (
              <>
                <span>{output.filename}</span>
                <span> · </span>
              </>
            ) : null}
            <span>{output.kind}</span>
            <span> · </span>
            <span>{formatTimestamp(output.createdAt)}</span>
          </div>
        </div>
      </header>
      <footer className="issue-outputs__card-actions">
        <Button
          variant="secondary" size="compact"
          onClick={() => {
            if (!token || !output.artifactId) return
            void downloadAuthenticated(
              artifactContentUrl(output.artifactId),
              token,
              output.filename ?? output.title
            )
          }}
          disabled={!token}
        >
          Download
        </Button>
        {source.conversationId && onOpenConversation ? (
          <Button
            variant="tertiary" size="compact"
            onClick={() => onOpenConversation(source.conversationId!)}
          >
            Open conversation
          </Button>
        ) : null}
        {source.workflowRunId && onOpenRun ? (
          <Button
            variant="tertiary" size="compact"
            onClick={() => onOpenRun(source.workflowRunId!)}
          >
            Open run detail
          </Button>
        ) : null}
        {source.taskRunId && onOpenTrace ? (
          <Button
            variant="tertiary" size="compact"
            onClick={() => onOpenTrace(source.taskRunId!)}
          >
            Run details
          </Button>
        ) : null}
      </footer>
    </article>
  )
}

interface OutputsListProps {
  outputs: IssueOutput[]
  token?: string | null
  onOpenConversation?: (conversationId: string) => void
  onOpenRun?: (workflowRunId: string) => void
  onOpenTrace?: (taskRunId: string) => void
}

export function OutputsList({ outputs, token, onOpenConversation, onOpenRun, onOpenTrace }: OutputsListProps) {
  if (outputs.length === 0) {
    return <p className="page-activity__empty">No results produced yet.</p>
  }
  return (
    <div className="issue-outputs__list">
      {outputs.map((o) => (
        <OutputCard
          key={o.id}
          output={o}
          token={token}
          onOpenConversation={onOpenConversation}
          onOpenRun={onOpenRun}
          onOpenTrace={onOpenTrace}
        />
      ))}
    </div>
  )
}
