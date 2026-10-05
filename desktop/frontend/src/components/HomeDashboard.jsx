import { folderBaseName, formatSessionMeta } from '../lib/format';
import { useT } from '../i18n';

export function HomeDashboard({ recentSessions, recentProjects, projectById, onSelectSession, onOpenProject, onCreateProject }) {
  const t = useT();
  return (
    <div className="page-home">
      <div className="page-home__header">
        <div>
          <h1 className="page-home__title">{t('home.title')}</h1>
          <p className="page-home__subtitle">{t('home.subtitle')}</p>
        </div>
        <button type="button" className="page-home__primary" onClick={onCreateProject}>
          {t('shell.newProject')}
        </button>
      </div>

      <div className="page-home__grid">
        <section className="page-home__section" aria-label={t('home.recentChats')}>
          <div className="page-home__section-head">
            <h2>{t('home.recentChats')}</h2>
          </div>
          {recentSessions.length === 0 ? (
            <p className="page-home__empty">{t('home.noRecentChats')}</p>
          ) : (
            <div className="page-home__list">
              {recentSessions.map((s) => {
                const project = projectById.get(s.project_id);
                return (
                  <button
                    key={s.id}
                    type="button"
                    className="page-home__item"
                    onClick={() => onSelectSession(s.id)}
                  >
                    <span className="page-home__item-title">{s.pinned ? '★ ' : ''}{s.title?.trim() || t('chat.untitled')}</span>
                    <span className="page-home__item-meta">
                      {project?.name || folderBaseName(s.workspace) || t('home.unknownProject')} · {formatSessionMeta(s.created_at, t)}
                    </span>
                  </button>
                );
              })}
            </div>
          )}
        </section>

        <section className="page-home__section" aria-label={t('home.recentProjects')}>
          <div className="page-home__section-head">
            <h2>{t('home.recentProjects')}</h2>
          </div>
          {recentProjects.length === 0 ? (
            <p className="page-home__empty">{t('home.noRecentProjects')}</p>
          ) : (
            <div className="page-home__list">
              {recentProjects.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className="page-home__item"
                  onClick={() => onOpenProject(p)}
                >
                  <span className="page-home__item-title">{p.name}</span>
                  <span className="page-home__item-meta">{p.default_workspace}</span>
                </button>
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

// --- ApprovalPanel ---
