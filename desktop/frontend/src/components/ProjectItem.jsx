import { useEffect, useRef, useState } from 'react';
import { useLocale } from '@buildmax/gui';
import { formatSessionMeta } from '../lib/format';
import { Chevron, MoreIcon, PinIcon, PlusIcon } from './icons';
import { AttentionDot } from './TabBar';
import { useT } from '../i18n';

export const SESSION_PAGE_SIZE = 10;

export function ProjectItem({ project, sessions, isActive, selectedSessionId, onSelectSession, onNewChat, onRename, onDelete, onClearSessions, onRenameSession, onDeleteSession, onPinSession, waitingSessions, waiting = false }) {
  const t = useT();
  const { locale } = useLocale();
  const [expanded, setExpanded] = useState(isActive);
  const [showMenu, setShowMenu] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [renameValue, setRenameValue] = useState(project.name);
  const [showAllSessions, setShowAllSessions] = useState(false);
  const [sessionMenuId, setSessionMenuId] = useState(null);
  const [renamingSessionId, setRenamingSessionId] = useState(null);
  const [sessionRenameValue, setSessionRenameValue] = useState('');
  const menuRef = useRef(null);
  const sessionMenuRef = useRef(null);

  useEffect(() => { if (isActive) setExpanded(true); }, [isActive]);
  useEffect(() => { setRenameValue(project.name); }, [project.name]);

  useEffect(() => {
    if (!showMenu) return;
    function onOutside(e) {
      if (menuRef.current && !menuRef.current.contains(e.target)) setShowMenu(false);
    }
    document.addEventListener('mousedown', onOutside);
    return () => document.removeEventListener('mousedown', onOutside);
  }, [showMenu]);

  useEffect(() => {
    if (!sessionMenuId) return;
    function onOutside(e) {
      if (sessionMenuRef.current && !sessionMenuRef.current.contains(e.target)) setSessionMenuId(null);
    }
    document.addEventListener('mousedown', onOutside);
    return () => document.removeEventListener('mousedown', onOutside);
  }, [sessionMenuId]);

  function startRename() {
    setShowMenu(false);
    setRenaming(true);
    setRenameValue(project.name);
  }

  async function submitRename() {
    const trimmed = renameValue.trim();
    setRenaming(false);
    if (trimmed && trimmed !== project.name) await onRename(project.id, trimmed);
  }

  function onRenameKeyDown(e) {
    if (e.key === 'Enter') submitRename();
    if (e.key === 'Escape') { setRenaming(false); setRenameValue(project.name); }
  }

  function handleDelete() {
    setShowMenu(false);
    onDelete(project.id);
  }

  function handleClearSessions() {
    setShowMenu(false);
    onClearSessions(sessions);
  }

  function startSessionRename(session) {
    setSessionMenuId(null);
    setRenamingSessionId(session.id);
    setSessionRenameValue(session.title?.trim() || t('chat.untitled'));
  }

  async function submitSessionRename(session) {
    const trimmed = sessionRenameValue.trim();
    setRenamingSessionId(null);
    if (trimmed && trimmed !== session.title) await onRenameSession(session.id, trimmed);
  }

  function onSessionRenameKeyDown(e, session) {
    if (e.key === 'Enter') submitSessionRename(session);
    if (e.key === 'Escape') {
      setRenamingSessionId(null);
      setSessionRenameValue('');
    }
  }

  function handleSessionDelete(session) {
    setSessionMenuId(null);
    onDeleteSession(session.id);
  }

  function handleSessionPin(session) {
    setSessionMenuId(null);
    onPinSession(session.id, !session.pinned);
  }

  return (
    <div className="sidebar__project">
      <div className={`sidebar__row${isActive ? ' sidebar__row--current' : ''}`}>
        <button
          type="button"
          className="sidebar__row-main"
          onClick={() => !renaming && setExpanded((e) => !e)}
          aria-expanded={expanded}
          title={project.default_workspace}
        >
          <span className="sidebar__row-icon"><Chevron open={expanded} /></span>
          {renaming ? (
            <input
              className="sidebar__rename-input"
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              onBlur={submitRename}
              onKeyDown={onRenameKeyDown}
              onClick={(e) => e.stopPropagation()}
              autoFocus
            />
          ) : (
            <span className="sidebar__row-label">{project.name}</span>
          )}
          {waiting && !renaming && <AttentionDot />}
        </button>

        <div className={`sidebar__row-actions${showMenu ? ' sidebar__row-actions--open' : ''}`} ref={menuRef}>
          <button
            type="button"
            className="sidebar__icon-btn"
            onClick={(e) => { e.stopPropagation(); setShowMenu((v) => !v); }}
            title={t('home.project.options')}
            aria-label={t('home.project.options')}
          >
            <MoreIcon />
          </button>
          <button
            type="button"
            className="sidebar__icon-btn"
            onClick={onNewChat}
            title={t('home.project.newChat')}
            aria-label={t('home.project.newChatIn', { name: project.name })}
          >
            <PlusIcon />
          </button>

          {showMenu && (
            <div className="context-menu" role="menu">
              <button type="button" className="context-menu__item" role="menuitem" onClick={startRename}>
                {t('shell.rename')}
              </button>
              <button type="button" className="context-menu__item" role="menuitem" onClick={handleClearSessions}>
                {t('home.project.clearSessions')}
              </button>
              <button type="button" className="context-menu__item context-menu__item--danger" role="menuitem" onClick={handleDelete}>
                {t('shell.remove')}
              </button>
            </div>
          )}
        </div>
      </div>

      {expanded && (
        <div className="sidebar__project-body">
          {(showAllSessions ? sessions : sessions.slice(0, SESSION_PAGE_SIZE)).map((s) => (
            <div
              key={s.id}
              className={`sidebar__row sidebar__row--nested${s.id === selectedSessionId ? ' sidebar__row--active' : ''}`}
            >
              {renamingSessionId === s.id ? (
                <input
                  className="sidebar__rename-input"
                  value={sessionRenameValue}
                  onChange={(e) => setSessionRenameValue(e.target.value)}
                  onBlur={() => submitSessionRename(s)}
                  onKeyDown={(e) => onSessionRenameKeyDown(e, s)}
                  autoFocus
                />
              ) : (
                <button
                  type="button"
                  className="sidebar__row-main"
                  onClick={() => onSelectSession(s.id)}
                  aria-current={s.id === selectedSessionId ? 'true' : undefined}
                  title={s.title || t('chat.untitled')}
                >
                  {s.pinned && <span className="sidebar__row-pin" aria-label={t('home.session.pinned')}><PinIcon /></span>}
                  <span className="sidebar__row-label">{s.title?.trim() || t('chat.untitled')}</span>
                  {waitingSessions?.has(s.id) && <AttentionDot />}
                  <span className="sidebar__row-meta">{formatSessionMeta(s.created_at, t, locale)}</span>
                </button>
              )}
              <div
                className={`sidebar__row-actions${sessionMenuId === s.id ? ' sidebar__row-actions--open' : ''}`}
                ref={sessionMenuId === s.id ? sessionMenuRef : null}
              >
                <button
                  type="button"
                  className="sidebar__icon-btn"
                  onClick={(e) => { e.stopPropagation(); setSessionMenuId((id) => id === s.id ? null : s.id); }}
                  title={t('home.session.options')}
                  aria-label={t('home.session.options')}
                >
                  <MoreIcon />
                </button>
                {sessionMenuId === s.id && (
                  <div className="context-menu" role="menu">
                    <button type="button" className="context-menu__item" role="menuitem" onClick={() => handleSessionPin(s)}>
                      {s.pinned ? t('home.session.unpin') : t('home.session.pin')}
                    </button>
                    <button type="button" className="context-menu__item" role="menuitem" onClick={() => startSessionRename(s)}>
                      {t('shell.rename')}
                    </button>
                    <button type="button" className="context-menu__item context-menu__item--danger" role="menuitem" onClick={() => handleSessionDelete(s)}>
                      {t('shell.delete')}
                    </button>
                  </div>
                )}
              </div>
            </div>
          ))}
          {!showAllSessions && sessions.length > SESSION_PAGE_SIZE && (
            <button
              type="button"
              className="sidebar__row sidebar__row--nested sidebar__row--more"
              onClick={() => setShowAllSessions(true)}
            >
              {t('shell.showMore', { count: sessions.length - SESSION_PAGE_SIZE })}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// --- CreateProjectModal ---
