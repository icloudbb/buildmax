import { useTheme } from '@buildmax/gui';
import { useEffect, useState } from 'react';
import { highlightToHtml } from '../lib/highlight';
import { MarkdownMessage } from './MarkdownMessage';

const MARKDOWN_RE = /\.(md|markdown)$/i;

// FileView renders one workspace file's content: it fetches the file on demand
// (App.ReadWorkspaceFile), highlights source, and offers a rendered view for
// Markdown. It is the shared content of both the Explorer's file tab and the
// legacy inspector file browser, so the two never diverge.
export function FileView({ projectID, sessionID, path, app }) {
  const { theme } = useTheme();
  const [file, setFile] = useState(null); // { content, binary, truncated, error, loading }
  const [viewMode, setViewMode] = useState('source'); // 'source' | 'preview' (Markdown only)
  const [highlightedHtml, setHighlightedHtml] = useState(null);
  const isMarkdown = MARKDOWN_RE.test(path);

  // Fetch the file whenever the path (or workspace) changes. A session may run
  // in a worktree distinct from the project default, so session is part of the
  // key.
  useEffect(() => {
    let cancelled = false;
    setViewMode('source');
    setHighlightedHtml(null);
    if (!path) { setFile(null); return undefined; }
    if (!app?.ReadWorkspaceFile) {
      setFile({ error: 'Rebuild the desktop app to preview files.', loading: false });
      return undefined;
    }
    setFile({ loading: true });
    app.ReadWorkspaceFile(projectID, sessionID, path)
      .then((res) => {
        if (cancelled) return;
        setFile({
          content: res?.content ?? '', binary: !!res?.binary,
          truncated: !!res?.truncated, error: res?.error ?? null, loading: false,
        });
      })
      .catch((err) => { if (!cancelled) setFile({ error: err?.message ?? String(err), loading: false }); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, path, app]);

  // Highlight source once content or theme settles; Markdown preview renders
  // through MarkdownMessage instead.
  useEffect(() => {
    let cancelled = false;
    if (!file || file.loading || file.error || file.binary || (isMarkdown && viewMode === 'preview')) {
      setHighlightedHtml(null);
      return undefined;
    }
    highlightToHtml(file.content, path, theme)
      .then((html) => { if (!cancelled) setHighlightedHtml(html); })
      .catch(() => { if (!cancelled) setHighlightedHtml(null); });
    return () => { cancelled = true; };
  }, [path, file, theme, isMarkdown, viewMode]);

  if (!path) return <p className="file-view__hint">Select a file to preview it.</p>;
  if (!file || file.loading) return <p className="file-view__hint">Loading…</p>;
  if (file.error) return <p className="file-view__hint file-view__hint--error">{file.error}</p>;
  if (file.binary) return <p className="file-view__hint">Binary file — no preview.</p>;

  return (
    <div className="file-view">
      <div className="file-view__header">
        <span className="file-view__path">{path}</span>
        {file.truncated && <span className="file-view__badge">truncated</span>}
        {isMarkdown && (
          <div className="file-view__mode" role="group" aria-label="View mode">
            <button
              type="button"
              className={`file-view__mode-btn ${viewMode === 'source' ? 'file-view__mode-btn--active' : ''}`}
              aria-pressed={viewMode === 'source'}
              onClick={() => setViewMode('source')}
            >
              Source
            </button>
            <button
              type="button"
              className={`file-view__mode-btn ${viewMode === 'preview' ? 'file-view__mode-btn--active' : ''}`}
              aria-pressed={viewMode === 'preview'}
              onClick={() => setViewMode('preview')}
            >
              Preview
            </button>
          </div>
        )}
      </div>
      {isMarkdown && viewMode === 'preview' ? (
        <div className="file-view__preview">
          <MarkdownMessage content={file.content} />
        </div>
      ) : highlightedHtml ? (
        <div className="file-view__code" dangerouslySetInnerHTML={{ __html: highlightedHtml }} />
      ) : (
        <pre className="file-view__code">{file.content || ' '}</pre>
      )}
    </div>
  );
}
