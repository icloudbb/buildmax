import { useEffect, useState } from 'react';
import { formatTokenCount, formatBytes } from '../lib/format';
import { MemoryView } from './MemoryDrawer';
import { useT } from '../i18n';

// buildStatRows turns the /info session payload into labelled lines, matching
// what the CLI command and the TUI panel report and honouring the same rules
// about what may be claimed — a cache saving only where it was real, timings
// only where a trace recorded them.
function buildStatRows(info, t) {
  const rows = [];
  const inOut = (prompt, completion) =>
    t('chat.info.inOut', { in: formatTokenCount(prompt), out: formatTokenCount(completion) });

  let spend = inOut(info.prompt_tokens, info.completion_tokens);
  spend += info.priced ? ` · ${info.cost_text} ${info.currency}` : ` · ${t('chat.info.notPriced')}`;
  rows.push([t('chat.info.spend'), spend]);

  if (info.cache_read_tokens > 0 || info.cache_write_tokens > 0) {
    let cache = t('chat.info.readWrite', {
      read: formatTokenCount(info.cache_read_tokens),
      write: formatTokenCount(info.cache_write_tokens),
    });
    if (info.cache_saved_text) cache += ` · ${t('chat.info.saved', { amount: info.cache_saved_text })}`;
    else if (info.cache_cost_more) cache += ` · ${t('chat.info.costMore')}`;
    rows.push([t('chat.info.cache'), cache]);
  }

  if (info.delegated_runs > 0) {
    let d = `${t('chat.info.runs', { count: info.delegated_runs })} · ${inOut(info.delegated_prompt_tokens, info.delegated_completion_tokens)}`;
    if (info.delegated_cost_text) d += ` · ${info.delegated_cost_text}`;
    rows.push([t('chat.info.delegated'), d]);
  }

  let ctx = info.peak_recorded
    ? t('chat.info.peak', {
        peak: formatTokenCount(info.peak_context_tokens),
        window: formatTokenCount(info.context_window),
        pct: Math.round(info.context_share * 100),
      })
    : t('chat.info.peakUnknown');
  ctx += ` · ${t('chat.info.compactions', { count: info.compactions })}`;
  rows.push([t('chat.info.context'), ctx]);

  rows.push([t('chat.info.historyLabel'), t('chat.info.textTool', {
    text: formatBytes(info.text_bytes),
    tool: formatBytes(info.tool_result_bytes),
  })]);

  let work = [
    t('chat.info.messages', { count: info.user_messages }),
    t('chat.info.turns', { count: info.assistant_turns }),
    t('chat.info.toolCalls', { count: info.tool_calls }),
  ].join(' · ');
  if (info.tool_failures > 0) work += ` · ${t('chat.info.failed', { count: info.tool_failures })}`;
  if (info.tool_denials > 0) work += ` · ${t('chat.info.denied', { count: info.tool_denials })}`;
  rows.push([t('chat.info.work'), work]);

  if (!info.has_trace) {
    rows.push([t('chat.info.time'), t('chat.info.noTrace')]);
  } else {
    let time = t('chat.info.waiting', { wall: info.wall_text });
    if (info.model_text) time += ` · ${t('chat.info.modelTools', { model: info.model_text, tools: info.tools_text })}`;
    else if (info.tools_overlap && info.tools_text) time += ` · ${t('chat.info.inTools', { tools: info.tools_text })}`;
    if (info.subagents > 0) time += ` · ${t('chat.info.subagentRuns', { count: info.subagents })}`;
    rows.push([t('chat.info.time'), time]);
  }
  return rows;
}

function SessionTab({ projectID, sessionID, app }) {
  const t = useT();
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
  if (!info) return <p className="diff-drawer__empty">{t('shell.loading')}</p>;
  if (info.load_error && info.prompt_tokens === 0 && !info.has_trace) {
    return <p className="diff-drawer__empty">{info.load_error}</p>;
  }

  const rows = buildStatRows(info, t);
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
          <p className="info-stats__subhead">{t('chat.info.heaviestTools')}</p>
          <table className="info-stats__tools">
            <tbody>
              {tools.map((tool) => (
                <tr key={tool.name || '(unattributed)'}>
                  <td className="info-stats__tool-name">{tool.name || t('chat.info.unattributed')}</td>
                  <td>{tool.calls}</td>
                  <td>{tool.result_bytes > 0 ? formatBytes(tool.result_bytes) : '—'}</td>
                  <td>{tool.wall_text || '—'}</td>
                  <td className="info-stats__tool-note">{tool.note || ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {(info.tools ?? []).length > tools.length && (
            <p className="info-stats__more">{t('chat.info.more', { count: (info.tools ?? []).length - tools.length })}</p>
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
  const t = useT();
  if (!node) return null;
  const title = node.missing ? t('chat.info.sourceDeleted') : (node.title || t('chat.info.untitled'));
  return (
    <li aria-current={node.current ? 'true' : undefined}>
      <div className={`info-tree__node ${node.current ? 'info-tree__node--current' : ''} ${node.missing ? 'info-tree__node--missing' : ''}`}>
        <span className="info-tree__title">{title}</span>
        <code className="info-tree__id">{node.id}</code>
        {node.current && <span className="info-tree__current">{t('chat.info.current')}</span>}
      </div>
      {(node.children ?? []).length > 0 && (
        <ul>
          {node.children.map((child) => <ForkTreeNode key={child.id} node={child} />)}
        </ul>
      )}
    </li>
  );
}

function TreeTab({ projectID, sessionID, app }) {
  const t = useT();
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
  if (!result) return <p className="diff-drawer__empty">{t('shell.loading')}</p>;
  if (result.load_error) return <p className="diff-drawer__empty">{result.load_error}</p>;
  if (!result.tree) return <p className="diff-drawer__empty">{t('chat.info.noForkTree')}</p>;

  return (
    <div className="info-tree">
      <p className="info-tree__hint">{t('chat.info.forkHint')}</p>
      {/* A read-only hierarchy is nested lists; an ARIA tree is an
          interactive widget with arrow-key navigation, which this is not. */}
      <ul>
        <ForkTreeNode node={result.tree} />
      </ul>
    </div>
  );
}

// InfoPanel is the /info command: what this session has done, where it sits in
// the fork tree, and what this project knows. See manual/cli.md.
export function InfoPanel({ projectID, sessionID, projectName, workspace, app }) {
  const t = useT();
  const [tab, setTab] = useState('session');

  return (
    <div className="info-panel">
      {(projectName || workspace) && (
        <div className="info-panel__meta">
          {projectName && <div className="info-panel__project">{projectName}</div>}
          {workspace && <div className="info-panel__path" title={workspace}>{workspace}</div>}
        </div>
      )}
      <div className="info-panel__tabs info-tabs" role="tablist" aria-label={t('chat.info.tabs')}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'session'}
          className={`info-tabs__tab ${tab === 'session' ? 'info-tabs__tab--active' : ''}`}
          onClick={() => setTab('session')}
        >
          {t('chat.info.session')}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'tree'}
          className={`info-tabs__tab ${tab === 'tree' ? 'info-tabs__tab--active' : ''}`}
          onClick={() => setTab('tree')}
        >
          {t('chat.info.tree')}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'memory'}
          className={`info-tabs__tab ${tab === 'memory' ? 'info-tabs__tab--active' : ''}`}
          onClick={() => setTab('memory')}
        >
          {t('chat.info.memory')}
        </button>
      </div>

      {tab === 'session' && (sessionID
        ? <SessionTab projectID={projectID} sessionID={sessionID} app={app} />
        : <p className="diff-drawer__empty">{t('chat.info.noStats')}</p>)}
      {tab === 'tree' && (sessionID
        ? <TreeTab projectID={projectID} sessionID={sessionID} app={app} />
        : <p className="diff-drawer__empty">{t('chat.info.noTree')}</p>)}
      {tab === 'memory' && <MemoryView projectID={projectID} app={app} />}
    </div>
  );
}
