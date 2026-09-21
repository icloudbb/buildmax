import { useEffect, useState } from 'react';
import { formatTokenCount, formatBytes } from '../lib/format';
import { MemoryView } from './MemoryDrawer';

function plural(n, noun) {
  return `${n} ${noun}${n === 1 ? '' : 's'}`;
}

// buildStatRows turns the /info session payload into labelled lines, matching
// what the CLI command and the TUI panel report and honouring the same rules
// about what may be claimed — a cache saving only where it was real, timings
// only where a trace recorded them.
function buildStatRows(info) {
  const rows = [];

  let spend = `${formatTokenCount(info.prompt_tokens)} in / ${formatTokenCount(info.completion_tokens)} out`;
  spend += info.priced ? ` · ${info.cost_text} ${info.currency}` : ' · not priced';
  rows.push(['Spend', spend]);

  if (info.cache_read_tokens > 0 || info.cache_write_tokens > 0) {
    let cache = `${formatTokenCount(info.cache_read_tokens)} read / ${formatTokenCount(info.cache_write_tokens)} write`;
    if (info.cache_saved_text) cache += ` · saved ${info.cache_saved_text}`;
    else if (info.cache_cost_more) cache += ' · cost more than uncached';
    rows.push(['Cache', cache]);
  }

  if (info.delegated_runs > 0) {
    let d = `${plural(info.delegated_runs, 'run')} · ${formatTokenCount(info.delegated_prompt_tokens)} in / ${formatTokenCount(info.delegated_completion_tokens)} out`;
    if (info.delegated_cost_text) d += ` · ${info.delegated_cost_text}`;
    rows.push(['Delegated', d]);
  }

  let ctx = info.peak_recorded
    ? `peak ${formatTokenCount(info.peak_context_tokens)} of ${formatTokenCount(info.context_window)} (${Math.round(info.context_share * 100)}%)`
    : 'peak not recorded';
  ctx += ` · ${plural(info.compactions, 'compaction')}`;
  rows.push(['Context', ctx]);

  rows.push(['History', `${formatBytes(info.text_bytes)} text / ${formatBytes(info.tool_result_bytes)} tool output`]);

  let work = `${plural(info.user_messages, 'message')} · ${plural(info.assistant_turns, 'turn')} · ${plural(info.tool_calls, 'tool call')}`;
  if (info.tool_failures > 0) work += ` · ${info.tool_failures} could not complete`;
  if (info.tool_denials > 0) work += ` · ${info.tool_denials} denied`;
  rows.push(['Work', work]);

  if (!info.has_trace) {
    rows.push(['Time', 'no trace recorded, so timings are unavailable']);
  } else {
    let time = `${info.wall_text} waiting`;
    if (info.model_text) time += ` · ${info.model_text} model / ${info.tools_text} tools`;
    else if (info.tools_overlap && info.tools_text) time += ` · ${info.tools_text} in tools, overlapping`;
    if (info.subagents > 0) time += ` · ${plural(info.subagents, 'subagent run')}`;
    rows.push(['Time', time]);
  }
  return rows;
}

function SessionTab({ projectID, sessionID, app }) {
  const [info, setInfo] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    let cancelled = false;
    app.GetSlashInfo(projectID, sessionID)
      .then((res) => { if (!cancelled) setInfo(res ?? null); })
      .catch((err) => { if (!cancelled) setError(err?.message ?? String(err)); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, app]);

  if (error) return <p className="diff-drawer__error">{error}</p>;
  if (!info) return <p className="diff-drawer__empty">Loading…</p>;
  if (info.load_error && info.prompt_tokens === 0 && !info.has_trace) {
    return <p className="diff-drawer__empty">{info.load_error}</p>;
  }

  const rows = buildStatRows(info);
  const tools = (info.tools ?? []).slice(0, 8);
  return (
    <div className="info-stats">
      <dl className="info-stats__list">
        {rows.map(([label, value]) => (
          <div key={label} className="info-stats__row">
            <dt className="info-stats__label">{label}</dt>
            <dd className="info-stats__value">{value}</dd>
          </div>
        ))}
      </dl>

      {tools.length > 0 && (
        <>
          <p className="info-stats__subhead">Tools, heaviest first</p>
          <table className="info-stats__tools">
            <tbody>
              {tools.map((t) => (
                <tr key={t.name || '(unattributed)'}>
                  <td className="info-stats__tool-name">{t.name || '(unattributed)'}</td>
                  <td>{t.calls}</td>
                  <td>{t.result_bytes > 0 ? formatBytes(t.result_bytes) : '—'}</td>
                  <td>{t.wall_text || '—'}</td>
                  <td className="info-stats__tool-note">{t.note || ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {(info.tools ?? []).length > tools.length && (
            <p className="info-stats__more">… {(info.tools ?? []).length - tools.length} more</p>
          )}
        </>
      )}

      {(info.caveats ?? []).map((c) => (
        <p key={c} className="info-stats__caveat">! {c}</p>
      ))}
    </div>
  );
}

function ForkTreeNode({ node }) {
  if (!node) return null;
  const title = node.missing ? 'Source session deleted' : (node.title || '(untitled)');
  return (
    <li role="treeitem" aria-current={node.current ? 'true' : undefined}>
      <div className={`info-tree__node ${node.current ? 'info-tree__node--current' : ''} ${node.missing ? 'info-tree__node--missing' : ''}`}>
        <span className="info-tree__title">{title}</span>
        <code className="info-tree__id">{node.id}</code>
        {node.current && <span className="info-tree__current">current</span>}
      </div>
      {(node.children ?? []).length > 0 && (
        <ul role="group">
          {node.children.map((child) => <ForkTreeNode key={child.id} node={child} />)}
        </ul>
      )}
    </li>
  );
}

function TreeTab({ projectID, sessionID, app }) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    let cancelled = false;
    app.GetSessionForkTree(projectID, sessionID)
      .then((res) => { if (!cancelled) setResult(res ?? null); })
      .catch((err) => { if (!cancelled) setError(err?.message ?? String(err)); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, app]);

  if (error) return <p className="diff-drawer__error">{error}</p>;
  if (!result) return <p className="diff-drawer__empty">Loading…</p>;
  if (result.load_error) return <p className="diff-drawer__empty">{result.load_error}</p>;
  if (!result.tree) return <p className="diff-drawer__empty">This session is not part of a saved fork tree.</p>;

  return (
    <div className="info-tree">
      <p className="info-tree__hint">This project’s fork branches. The current session is highlighted.</p>
      <ul role="tree">
        <ForkTreeNode node={result.tree} />
      </ul>
    </div>
  );
}

// InfoPanel is the /info command: what this session has done, where it sits in
// the fork tree, and what this project knows. See manual/cli.md.
export function InfoPanel({ projectID, sessionID, projectName, workspace, app }) {
  const [tab, setTab] = useState('session');

  return (
    <div className="info-panel">
      {(projectName || workspace) && (
        <div className="info-panel__meta">
          {projectName && <div className="info-panel__project">{projectName}</div>}
          {workspace && <div className="info-panel__path" title={workspace}>{workspace}</div>}
        </div>
      )}
      <div className="info-panel__tabs info-tabs" role="tablist" aria-label="Info">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'session'}
          className={`info-tabs__tab ${tab === 'session' ? 'info-tabs__tab--active' : ''}`}
          onClick={() => setTab('session')}
        >
          Session
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'tree'}
          className={`info-tabs__tab ${tab === 'tree' ? 'info-tabs__tab--active' : ''}`}
          onClick={() => setTab('tree')}
        >
          Tree
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'memory'}
          className={`info-tabs__tab ${tab === 'memory' ? 'info-tabs__tab--active' : ''}`}
          onClick={() => setTab('memory')}
        >
          Memory
        </button>
      </div>

      {tab === 'session' && (sessionID
        ? <SessionTab projectID={projectID} sessionID={sessionID} app={app} />
        : <p className="diff-drawer__empty">Send a message first — this session has no statistics yet.</p>)}
      {tab === 'tree' && (sessionID
        ? <TreeTab projectID={projectID} sessionID={sessionID} app={app} />
        : <p className="diff-drawer__empty">Send a message first — this session has no fork tree yet.</p>)}
      {tab === 'memory' && <MemoryView projectID={projectID} app={app} />}
    </div>
  );
}
