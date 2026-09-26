import { useCallback, useEffect, useRef, useState } from 'react';
import { Avatar } from '@buildmax/gui';
import { ProjectItem } from './ProjectItem';
import { Explorer } from './Explorer';
import { SidebarSectionHeader } from './SidebarSection';
import { ClockIcon, HomeIcon, PlusIcon, SearchIcon } from './icons';
import { readStored, writeStored } from '../lib/storage';

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
// Schedules), sections (Projects, then the active project's own section), and
// rows. The project section exists only while a project workspace is the
// center view, because everything in it is scoped to that project.
export function Sidebar({
  width,
  view,
  onHome,
  onSchedules,
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
}) {
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
    <aside className="sidebar" aria-label="Sidebar" style={{ width }} ref={asideRef}>
      <nav className="sidebar__destinations" aria-label="Primary">
        <button
          type="button"
          className={`sidebar__row sidebar__row-main sidebar__destination${onHomeView ? ' sidebar__row--active' : ''}`}
          onClick={onHome}
          aria-current={onHomeView ? 'page' : undefined}
        >
          <span className="sidebar__row-icon"><HomeIcon /></span>
          <span className="sidebar__row-label">Home</span>
        </button>
        <button
          type="button"
          className={`sidebar__row sidebar__row-main sidebar__destination${view === 'schedules' ? ' sidebar__row--active' : ''}`}
          onClick={onSchedules}
          aria-current={view === 'schedules' ? 'page' : undefined}
        >
          <span className="sidebar__row-icon"><ClockIcon /></span>
          <span className="sidebar__row-label">Schedules</span>
        </button>
      </nav>

      <section className={`sidebar__projects ${projectsFlex}`} aria-label="Projects">
        <SidebarSectionHeader label="Projects" open={projectsOpen} onToggle={() => setProjectsOpen((v) => !v)}>
          <button
            type="button"
            className="sidebar__icon-btn"
            aria-pressed={searchVisible}
            onClick={() => (searchVisible ? closeSearch() : setSearchOpen(true))}
            title="Search sessions"
            aria-label="Search sessions"
          >
            <SearchIcon />
          </button>
          <button
            type="button"
            className="sidebar__icon-btn"
            onClick={onCreateProject}
            title="New Project"
            aria-label="New Project"
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
                  placeholder="Search sessions"
                  aria-label="Search sessions"
                  autoFocus
                />
              </div>
            )}
            <div className="sidebar__section-body">
              {projects.length === 0 ? (
                <button type="button" className="sidebar__row sidebar__row-main" onClick={onCreateProject}>
                  <span className="sidebar__row-icon"><PlusIcon /></span>
                  <span className="sidebar__row-label">New Project</span>
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
                    />
                  ))}
                  {!showAllProjects && projects.length > PROJECT_PAGE_SIZE && (
                    <button
                      type="button"
                      className="sidebar__row sidebar__row--more"
                      onClick={() => setShowAllProjects(true)}
                    >
                      Show {projects.length - PROJECT_PAGE_SIZE} more…
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
              aria-label="Resize project section"
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
          aria-label="User menu"
        >
          <Avatar
            label={(authStatus.name?.trim() || authStatus.email || 'Local').slice(0, 1).toUpperCase()}
            size="sm"
          />
          <span className="sidebar__user-name">
            {localMode
              ? 'Local mode'
              : authStatus.name?.trim() || (authStatus.email ? authStatus.email.split('@')[0] : '')}
          </span>
        </button>
        {userMenuOpen && (
          <div className="sidebar__user-menu" role="menu">
            <div className="sidebar__user-menu-email">
              {localMode ? 'Models from settings.yaml' : authStatus.email}
            </div>
            {!localMode && authStatus.server_url && (
              <div className="sidebar__user-menu-server">Prompts go to {authStatus.server_url}</div>
            )}
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
              {localMode ? 'Sign in to a server' : 'Sign out'}
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}
