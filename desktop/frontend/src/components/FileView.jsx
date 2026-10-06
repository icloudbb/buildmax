import { useTheme } from '@buildmax/gui';
import { useEffect, useState } from 'react';
import { highlightToHtml } from '../lib/highlight';
import { MarkdownMessage } from './MarkdownMessage';
import { useStableT, useT } from '../i18n';

const MARKDOWN_RE = /\.(md|markdown)$/i;

// FileView renders one workspace file's content: it fetches the file on demand
// (App.ReadWorkspaceFile), highlights source, offers a rendered view for
// Markdown, and — when the backend exposes WriteWorkspaceFile — lets the user
// edit and save it. Editing is refused for binary or truncated files, so a save
// never rewrites content the preview only partly holds.
export function FileView({ projectID, sessionID, path, app }) {
  const { theme } = useTheme();
  const t = useT();
  const stableT = useStableT();
  const [file, setFile] = useState(null); // { content, binary, truncated, error, loading }
  const [viewMode, setViewMode] = useState('source'); // 'source' | 'preview' (Markdown only)
  const [highlightedHtml, setHighlightedHtml] = useState(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState(null);
  const isMarkdown = MARKDOWN_RE.test(path);

  // Fetch the file whenever the path (or workspace) changes. A session may run
  // in a worktree distinct from the project default, so session is part of the
  // key.
  useEffect(() => {
    let cancelled = false;
    setViewMode('source');
    setHighlightedHtml(null);
    setEditing(false);
    setSaveError(null);
    if (!path) { setFile(null); return undefined; }
    if (!app?.ReadWorkspaceFile) {
      setFile({ error: stableT('files.rebuildPreview'), loading: false });
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
  }, [projectID, sessionID, path, app, stableT]);

  // Highlight source once content or theme settles; Markdown preview and the
  // editor render through their own paths instead.
  useEffect(() => {
    let cancelled = false;
    if (!file || file.loading || file.error || file.binary || editing || (isMarkdown && viewMode === 'preview')) {
      setHighlightedHtml(null);
      return undefined;
    }
    highlightToHtml(file.content, path, theme)
      .then((html) => { if (!cancelled) setHighlightedHtml(html); })
      .catch(() => { if (!cancelled) setHighlightedHtml(null); });
    return () => { cancelled = true; };
  }, [path, file, theme, isMarkdown, viewMode, editing]);

  const canEdit = !!app?.WriteWorkspaceFile && file && !file.loading && !file.error && !file.binary && !file.truncated;

  function startEdit() {
    setDraft(file.content ?? '');
    setSaveError(null);
    setEditing(true);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);
    try {
      const res = await app.WriteWorkspaceFile(projectID, sessionID, path, draft);
      if (res?.error) { setSaveError(res.error); return; }
      setFile({
        content: res?.content ?? draft, binary: !!res?.binary,
        truncated: !!res?.truncated, error: null, loading: false,
      });
      setEditing(false);
    } catch (err) {
      setSaveError(err?.message ?? String(err));
    } finally {
      setSaving(false);
    }
  }

  if (!path) return <p className="file-view__hint">{t('files.selectFile')}</p>;
  if (!file || file.loading) return <p className="file-view__hint">{t('shell.loading')}</p>;
  if (file.error) return <p className="file-view__hint file-view__hint--error">{file.error}</p>;
  if (file.binary) return <p className="file-view__hint">{t('files.binary')}</p>;

  return (
    <div className="file-view">
      <div className="file-view__header">
        <span className="file-view__path">{path}</span>
        {file.truncated && <span className="file-view__badge">{t('files.truncated')}</span>}
        {saveError && <span className="file-view__badge file-view__badge--error">{saveError}</span>}
        {!editing && isMarkdown && (
          <div className="file-view__mode" role="group" aria-label={t('files.viewMode')}>
            <button
              type="button"
              className={`file-view__mode-btn ${viewMode === 'source' ? 'file-view__mode-btn--active' : ''}`}
              aria-pressed={viewMode === 'source'}
              onClick={() => setViewMode('source')}
            >
              {t('files.source')}
            </button>
            <button
              type="button"
              className={`file-view__mode-btn ${viewMode === 'preview' ? 'file-view__mode-btn--active' : ''}`}
              aria-pressed={viewMode === 'preview'}
              onClick={() => setViewMode('preview')}
            >
              {t('files.preview')}
            </button>
          </div>
        )}
        {editing ? (
          <div className="file-view__mode" role="group" aria-label={t('files.editActions')}>
            <button type="button" className="file-view__mode-btn file-view__mode-btn--active" onClick={save} disabled={saving}>
              {saving ? t('files.saving') : t('files.save')}
            </button>
            <button
              type="button"
              className="file-view__mode-btn"
              onClick={() => { setEditing(false); setSaveError(null); }}
              disabled={saving}
            >
              {t('shell.cancel')}
            </button>
          </div>
        ) : canEdit && (
          <button type="button" className="file-view__mode-btn file-view__edit" onClick={startEdit}>
            {t('files.edit')}
          </button>
        )}
      </div>
      {editing ? (
        <textarea
          className="file-view__editor"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          spellCheck={false}
          aria-label={t('files.editPath', { path })}
        />
      ) : isMarkdown && viewMode === 'preview' ? (
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
