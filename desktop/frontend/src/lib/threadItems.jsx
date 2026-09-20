import { formatToolArgs, shortToolArgs, toolDisplayName } from './format';
import { buildToolResultMap } from './messages';
import { MarkdownMessage } from '../components/MarkdownMessage';

// messageThreadItems turns a session's messages into ChatThread items in the
// chat display style: assistant/user bubbles, background-event details, and each
// assistant tool call folded with its result. It is the shared rendering behind
// the live chat (ChatSession) and the read-and-continue view of a scheduled
// run, so both look identical. Callers append their own extra items (queued
// prompts, notices) to the returned array.
export function messageThreadItems(messages) {
  const toolResults = buildToolResultMap(messages);
  return messages.flatMap((m, i) => {
    if (m.role === 'tool') return [];
    const toolCallLines = (m.tool_calls || []).map((tc, j) => {
      const result = toolResults.get(tc.id);
      const state = result ? (result.ok ? 'success' : 'error') : 'pending';
      const args = shortToolArgs(tc.arguments);
      return (
        <details key={tc.id || j} className={`page-chat__tool-call page-chat__tool-call--${state}`}>
          <summary>
            <span className="page-chat__tool-call-dot" aria-hidden />
            <span className="page-chat__tool-call-name">{toolDisplayName(tc.name)}</span>
            {args && <span className="page-chat__tool-call-args">({args})</span>}
          </summary>
          {tc.arguments && (
            <pre className="page-chat__tool-call-block">{formatToolArgs(tc.arguments)}</pre>
          )}
          {result?.content && (
            <div className="page-chat__tool-call-result">
              <MarkdownMessage content={result.content} />
            </div>
          )}
        </details>
      );
    });
    if (m.source) {
      return [{
        id: `message-${i}`,
        role: m.role,
        label: 'Background',
        hideAvatar: true,
        body: (
          <details className="page-chat__msg-content">
            <summary>⟳ {m.source}</summary>
            {m.content ? <MarkdownMessage content={m.content} /> : null}
          </details>
        ),
      }];
    }
    return [{
      id: `message-${i}`,
      role: m.role,
      label: m.role === 'user' ? 'You' : m.role,
      hideAvatar: true,
      body: (
        <div className="page-chat__msg-content">
          {m.content ? <MarkdownMessage content={m.content} /> : null}
          {toolCallLines}
        </div>
      ),
    }];
  });
}
