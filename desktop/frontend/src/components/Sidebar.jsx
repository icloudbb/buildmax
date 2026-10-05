import { useCallback, useEffect, useRef, useState } from 'react';
import { Avatar } from '@buildmax/gui';
import { ProjectItem } from './ProjectItem';
import { Explorer } from './Explorer';
import { SidebarSectionHeader } from './SidebarSection';
import { ClockIcon, HomeIcon, IssueIcon, PlusIcon, SearchIcon } from './icons';
import { readStored, writeStored } from '../lib/storage';
import { LOCALES, LOCALE_NAMES, useLocale } from '@buildmax/gui';
import { useT } from '../i18n';

const PROJECT_PAGE_SIZE = 10;

// The project section's height is a per-machine preference like the sidebar
// width. The bounds keep both sections usable whatever the window height.
const LS_PROJECT_SECTION_HEIGHT = 'bm.desktop.projectSectionHeight';
const PROJECT_SECTION_DEFAULT_HEIGHT = 320;
const PROJECT_SECTION_MIN_HEIGHT = 96;
const PROJECTS_MIN_HEIGHT = 160;

function clampSectionHeight(h) {
  const n = Number(h);
  return Number.isFinite(n) ? Math.max(PROJECT_SECTION_MIN_HEIGHT, n) : PROJECT_SECTION_DEFAULT_HEIGHT;
}

// Sidebar has three layers, each with one look: global destinations (Home,
// Schedules, and Issues when signed in to a server), sections (Projects, then the active project's own section), and
// rows. The project section exists only while a project workspace is the
// center view, because everything in it is scoped to that project.
export function Sidebar({
  width,
  view,
  onHome,
  onSchedules,
  onIssues,
  projects,
  currentProject,
  sessionsByProject,
  highlightSessionId,
  sessionFilter,
  onSessionFilterChange,
  onCreateProject,
  projectActions,
  explorer,
  account,
  waiting,
}) {
  const t = useT();
  const { locale, setLocale } = useLocale();
  const [projectsOpen, setProjectsOpen] = useState(true);
  const [projectSectionOpen, setProjectSectionOpen] = useState(true);
  const [searchOpen, setSearchOpen] = useState(false);
  const [showAllProjects, setShowAllProjects] = useState(false);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [sectionHeight, setSectionHeight] = useState(() =>
    clampSectionHeight(readStored(LS_PROJECT_SECTION_HEIGHT, PROJECT_SECTION_DEFAULT_HEIGHT)),
  );
  const asideRef = useRef(null);
  const userMenuRef = useRef(null);

  useEffect(() => { writeStored(LS_PROJECT_SECTION_HEIGHT, sectionHeight); }, [sectionHeight]);

  // A mode switch (a click, or /diff from a chat) is a request to see the
  // section, so it reopens a collapsed one.
  const explorerMode = explorer.mode;
  useEffect(() => { setProjectSectionOpen(true); }, [explorerMode]);

  useEffect(() => {
    if (!userMenuOpen) return undefined;
    function handleClickOutside(e) {
      if (userMenuRef.current && !userMenuRef.current.contains(e.target)) setUserMenuOpen(false);
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [userMenuOpen]);

  const startSectionResize = useCallback((e) => {
    e.preventDefault();
    const startY = e.clientY;
    const startH = sectionHeight;
    const max = Math.max(PROJECT_SECTION_MIN_HEIGHT, (asideRef.current?.clientHeight ?? 0) - PROJECTS_MIN_HEIGHT);
    const onMove = (ev) => setSectionHeight(Math.round(Math.min(max, clampSectionHeight(startH + startY - ev.clientY))));
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      document.body.style.userSelect = '';
    };
    document.body.style.userSelect = 'none';
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, [sectionHeight]);

  function closeSearch() {
    setSearchOpen(false);
    onSessionFilterChange('');
  }

  const onHomeView = view === 'workbench' && !currentProject;
  const showProjectSection = view === 'workbench' && !!currentProject;
  // Selection marks what the center shows, so another destination clears it.
  const selectedSessionId = view === 'workbench' ? highlightSessionId : null;
  const searchVisible = searchOpen || sessionFilter !== '';
  const visibleProjects = showAllProjects ? projects : projects.slice(0, PROJECT_PAGE_SIZE);

  // Flex roles: an open section with nothing to share fills the height; with
  // both open, the project section keeps its dragged height and Projects takes
  // the rest.
  let projectsFlex = 'sidebar__projects--fill';
  let sectionStyle;
  if (!projectsOpen) projectsFlex = 'sidebar__projects--closed';
  if (showProjectSection && projectSectionOpen) {
    if (projectsOpen) sectionStyle = { flex: `0 0 ${sectionHeight}px` };
    else sectionStyle = { flex: '1 1 auto' };
  }

  const { authStatus, localMode, onSignIn, onSignOut } = account;

  return (
    <aside className="sidebar" aria-label={t('shell.sidebar')} style={{ width }} ref={asideRef}>
      <nav className="sidebar__destinations" aria-label={t('shell.primary')}>
        <button
          type="button"
          className={`sidebar__row sidebar__row-main sidebar__destination${onHomeView ? ' sidebar__row--active' : ''}`}
          onClick={onHome}
          aria-current={onHomeView ? 'page' : undefined}
        >
          <span className="sidebar__row-icon"><HomeIcon /></span>
          <span className="sidebar__row-label">{t('shell.nav.home')}</span>
        </button>
        <button
          type="button"
          className={`sidebar__row sidebar__row-main sidebar__destination${view === 'schedules' ? ' sidebar__row--active' : ''}`}
          onClick={onSchedules}
          aria-current={view === 'schedules' ? 'page' : undefined}
        >
          <span className="sidebar__row-icon"><ClockIcon /></span>
          <span className="sidebar__row-label">{t('shell.nav.schedules')}</span>
        </button>
        {/* Space Issues exist only on a server, so the destination does too:
            local mode has no work to receive and shows no empty promise of it. */}
        {onIssues && (
          <button
            type="button"
            className={`sidebar__row sidebar__row-main sidebar__destination${view === 'issues' ? ' sidebar__row--active' : ''}`}
            onClick={onIssues}
            aria-current={view === 'issues' ? 'page' : undefined}
          >
            <span className="sidebar__row-icon"><IssueIcon /></span>
            <span className="sidebar__row-label">{t('shell.nav.issues')}</span>
          </button>
        )}
      </nav>

      <section className={`sidebar__projects ${projectsFlex}`} aria-label={t('shell.projects')}>
        <SidebarSectionHeader label={t('shell.projects')} open={projectsOpen} onToggle={() => setProjectsOpen((v) => !v)} attention={(waiting?.projects.size ?? 0) > 0}>
          <button
            type="button"
            className="sidebar__icon-btn"
            aria-pressed={searchVisible}
            onClick={() => (searchVisible ? closeSearch() : setSearchOpen(true))}
            title={t('shell.searchSessions')}
            aria-label={t('shell.searchSessions')}
          >
            <SearchIcon />
          </button>
          <button
            type="button"
            className="sidebar__icon-btn"
            onClick={onCreateProject}
            title={t('shell.newProject')}
            aria-label={t('shell.newProject')}
          >
            <PlusIcon />
          </button>
        </SidebarSectionHeader>

        {projectsOpen && (
          <>
            {searchVisible && (
              <div className="sidebar__search">
                <input
                  id="sidebar-session-search"
                  type="search"
                  className="sidebar__search-input"
                  value={sessionFilter}
                  onChange={(e) => onSessionFilterChange(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Escape') closeSearch(); }}
                  placeholder={t('shell.searchSessions')}
                  aria-label={t('shell.searchSessions')}
                  autoFocus
                />
              </div>
            )}
            <div className="sidebar__section-body">
              {projects.length === 0 ? (
                <button type="button" className="sidebar__row sidebar__row-main" onClick={onCreateProject}>
                  <span className="sidebar__row-icon"><PlusIcon /></span>
                  <span className="sidebar__row-label">{t('shell.newProject')}</span>
                </button>
              ) : (
                <>
                  {visibleProjects.map((proj) => (
                    <ProjectItem
                      key={proj.id}
                      project={proj}
                      sessions={sessionsByProject[proj.id] ?? []}
                      isActive={showProjectSection && currentProject.id === proj.id}
                      selectedSessionId={selectedSessionId}
                      onSelectSession={projectActions.onSelectSession}
                      onNewChat={() => projectActions.onNewChat(proj)}
                      onRename={projectActions.onRename}
                      onDelete={projectActions.onDelete}
                      onClearSessions={(projectSessions) => projectActions.onClearSessions(proj, projectSessions)}
                      onRenameSession={projectActions.onRenameSession}
                      onDeleteSession={projectActions.onDeleteSession}
                      onPinSession={projectActions.onPinSession}
                      waitingSessions={waiting?.sessions}
                      waiting={!!waiting?.projects.has(proj.id)}
                    />
                  ))}
                  {!showAllProjects && projects.length > PROJECT_PAGE_SIZE && (
                    <button
                      type="button"
                      className="sidebar__row sidebar__row--more"
                      onClick={() => setShowAllProjects(true)}
                    >
                      {t('shell.showMore', { count: projects.length - PROJECT_PAGE_SIZE })}
                    </button>
                  )}
                </>
              )}
            </div>
          </>
        )}
      </section>

      {showProjectSection && (
        <>
          {projectsOpen && projectSectionOpen && (
            <div
              className="sidebar__split"
              role="separator"
              aria-orientation="horizontal"
              aria-label={t('shell.resizeProjectSection')}
              onMouseDown={startSectionResize}
            />
          )}
          <Explorer
            projectID={currentProject.id}
            projectName={currentProject.name}
            sessionID={explorer.sessionID}
            app={explorer.app}
            mode={explorer.mode}
            onModeChange={explorer.onModeChange}
            open={projectSectionOpen}
            onToggle={() => setProjectSectionOpen((v) => !v)}
            style={sectionStyle}
            onOpenFile={explorer.onOpenFile}
            onOpenDiff={explorer.onOpenDiff}
            onOpenCommitDiff={explorer.onOpenCommitDiff}
          />
        </>
      )}

      <div className="sidebar__footer" ref={userMenuRef}>
        <button
          type="button"
          className="sidebar__user-trigger"
          onClick={() => setUserMenuOpen((v) => !v)}
          aria-expanded={userMenuOpen}
          aria-haspopup="menu"
          aria-label={t('shell.userMenu')}
        >
          <Avatar
            label={(authStatus.name?.trim() || authStatus.email || 'Local').slice(0, 1).toUpperCase()}
            size="sm"
          />
          <span className="sidebar__user-name">
            {localMode
              ? t('shell.localMode')
              : authStatus.name?.trim() || (authStatus.email ? authStatus.email.split('@')[0] : '')}
          </span>
        </button>
        {userMenuOpen && (
          <div className="sidebar__user-menu" role="menu">
            <div className="sidebar__user-menu-email">
              {localMode ? t('shell.localModels') : authStatus.email}
            </div>
            {!localMode && authStatus.server_url && (
              <div className="sidebar__user-menu-server">{t('shell.promptsGoTo', { server: authStatus.server_url })}</div>
            )}
            <div className="sidebar__user-menu-divider" />
            <div className="sidebar__user-menu-server">{t('shell.language')}</div>
            {LOCALES.map((option) => (
              <button
                key={option}
                type="button"
                className="sidebar__user-menu-item"
                role="menuitemradio"
                aria-checked={locale === option}
                lang={option}
                onClick={() => {
                  setUserMenuOpen(false);
                  setLocale(option);
                }}
              >
                {locale === option ? '✓ ' : ''}{LOCALE_NAMES[option]}
              </button>
            ))}
            <div className="sidebar__user-menu-divider" />
            <button
              type="button"
              className="sidebar__user-menu-item"
              role="menuitem"
              onClick={() => {
                setUserMenuOpen(false);
                if (localMode) onSignIn();
                else onSignOut();
              }}
            >
              {localMode ? t('shell.signIn') : t('shell.signOut')}
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}
