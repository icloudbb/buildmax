// One line-icon set for the Desktop shell: 24-unit viewBox, 1.6 stroke, drawn
// in currentColor so each control sets size and colour from its own CSS.
// Text glyphs (« ▸ ··· ☰) render at different weights and baselines per
// platform font, which is why the shell does not use them for controls.
function Icon({ children, className }) {
  return (
    <svg
      className={className ? `icon ${className}` : 'icon'}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      {children}
    </svg>
  );
}

export function HomeIcon(props) {
  return <Icon {...props}><path d="M4 10.5 12 4l8 6.5V20a1 1 0 0 1-1 1h-4.5v-6h-5v6H5a1 1 0 0 1-1-1z" /></Icon>;
}

export function ClockIcon(props) {
  return <Icon {...props}><circle cx="12" cy="12" r="8.5" /><path d="M12 7.5V12l3 2" /></Icon>;
}

export function ChevronRightIcon(props) {
  return <Icon {...props}><path d="m9.5 6 6 6-6 6" /></Icon>;
}

export function ChevronDownIcon(props) {
  return <Icon {...props}><path d="m6 9.5 6 6 6-6" /></Icon>;
}

// Chevron picks its direction from `open`, the one question every tree row asks.
export function Chevron({ open, ...props }) {
  return open ? <ChevronDownIcon {...props} /> : <ChevronRightIcon {...props} />;
}

export function PlusIcon(props) {
  return <Icon {...props}><path d="M12 5.5v13M5.5 12h13" /></Icon>;
}

export function MoreIcon(props) {
  return (
    <Icon {...props}>
      <circle cx="6" cy="12" r="0.9" />
      <circle cx="12" cy="12" r="0.9" />
      <circle cx="18" cy="12" r="0.9" />
    </Icon>
  );
}

export function SearchIcon(props) {
  return <Icon {...props}><circle cx="11" cy="11" r="6" /><path d="m20 20-4.6-4.6" /></Icon>;
}

export function SidebarIcon(props) {
  return <Icon {...props}><rect x="3.5" y="4.5" width="17" height="15" rx="2" /><path d="M9.5 4.5v15" /></Icon>;
}

export function FilesIcon(props) {
  return (
    <Icon {...props}>
      <path d="M8.5 3.5h6l4 4v10a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1v-13a1 1 0 0 1 1-1z" />
      <path d="M14.5 3.5v4h4" />
      <path d="M5 7v12.5A1.5 1.5 0 0 0 6.5 21H15" />
    </Icon>
  );
}

export function SourceControlIcon(props) {
  return (
    <Icon {...props}>
      <circle cx="7" cy="5.5" r="2" />
      <circle cx="7" cy="18.5" r="2" />
      <circle cx="17" cy="8" r="2" />
      <path d="M7 7.5v9" />
      <path d="M17 10c0 4-4 4.5-10 6.5" />
    </Icon>
  );
}

export function FileIcon(props) {
  return (
    <Icon {...props}>
      <path d="M6.5 3.5h7l4 4v12a1 1 0 0 1-1 1h-10a1 1 0 0 1-1-1v-15a1 1 0 0 1 1-1z" />
      <path d="M13.5 3.5v4h4" />
    </Icon>
  );
}

export function FolderIcon(props) {
  return <Icon {...props}><path d="M3.5 7A1.5 1.5 0 0 1 5 5.5h4l2 2h8A1.5 1.5 0 0 1 20.5 9v9a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 18z" /></Icon>;
}

export function PinIcon(props) {
  return <Icon {...props}><path d="m12 4 2.3 4.8 5.2.7-3.8 3.6.9 5.2L12 15.8l-4.6 2.5.9-5.2-3.8-3.6 5.2-.7z" /></Icon>;
}

export function SunIcon(props) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </Icon>
  );
}

export function MoonIcon(props) {
  return <Icon {...props}><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" /></Icon>;
}

// GridIcon — tile tabs into a grid of panes.
export function GridIcon(props) {
  return (
    <Icon {...props}>
      <rect x="3" y="3" width="8" height="8" rx="1" />
      <rect x="13" y="3" width="8" height="8" rx="1" />
      <rect x="3" y="13" width="8" height="8" rx="1" />
      <rect x="13" y="13" width="8" height="8" rx="1" />
    </Icon>
  );
}

// SplitRightIcon — collapsing a grid back into one pane. A single frame divided
// left from right reads cleaner than a tab-strip drawing at this size.
export function SplitRightIcon(props) {
  return <Icon {...props}><rect x="3" y="3" width="18" height="18" rx="1" /><path d="M12 3v18" /></Icon>;
}
