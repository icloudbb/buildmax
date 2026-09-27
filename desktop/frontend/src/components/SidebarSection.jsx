import { Chevron } from './icons';
import { AttentionDot } from './TabBar';

// SidebarSectionHeader is the one header every sidebar section uses: a
// collapsing label on the left and the section's own actions on the right.
// attention marks a collapsed section that hides something waiting on the user.
export function SidebarSectionHeader({ label, open, onToggle, title, children, attention = false }) {
  return (
    <div className="sidebar__section-header">
      <button
        type="button"
        className="sidebar__section-toggle"
        onClick={onToggle}
        aria-expanded={open}
        title={title}
      >
        <Chevron open={open} />
        <span className="sidebar__section-label">{label}</span>
        {attention && !open && <AttentionDot />}
      </button>
      {children && <div className="sidebar__section-actions">{children}</div>}
    </div>
  );
}
