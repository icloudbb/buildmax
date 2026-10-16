import type { ReactNode, SVGProps } from "react"

// The one icon set for Portal, Desktop, and gui: a 24-unit viewBox and a 1.6
// line in currentColor, so a control sets an icon's color from its own text
// and its size from CSS (the 18px attributes are only the unstyled default).
// Text glyphs and emoji render at different weights and baselines per
// platform font, which is why controls use these instead.

export type IconProps = Omit<SVGProps<SVGSVGElement>, "children">

function Icon({ className, children, ...props }: IconProps & { children: ReactNode }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={18}
      height={18}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...props}
      className={className ? `bm-icon ${className}` : "bm-icon"}
    >
      {children}
    </svg>
  )
}

export function AgentsIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M9 3h6M12 3v2" />
      <rect x="4.5" y="5.5" width="15" height="13" rx="2.5" />
      <circle cx="9" cy="12" r="1" fill="currentColor" stroke="none" />
      <circle cx="15" cy="12" r="1" fill="currentColor" stroke="none" />
      <path d="M9.5 16.5h5" />
    </Icon>
  )
}

export function ArtifactIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="3.5" width="18" height="4.5" rx="1.5" />
      <path d="M4.5 8v10.5a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V8" />
      <path d="M10 12.5h4" />
    </Icon>
  )
}

export function ChatIcon(props: IconProps) {
  return <Icon {...props}><path d="M20 14.5a2 2 0 0 1-2 2H8l-4 3.5V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2z" /></Icon>
}

export function ChevronDownIcon(props: IconProps) {
  return <Icon {...props}><path d="m6 9.5 6 6 6-6" /></Icon>
}

export function ChevronRightIcon(props: IconProps) {
  return <Icon {...props}><path d="m9.5 6 6 6-6 6" /></Icon>
}

export function CloseIcon(props: IconProps) {
  return <Icon {...props}><path d="M6.5 6.5l11 11M17.5 6.5l-11 11" /></Icon>
}

export function CommitIcon(props: IconProps) {
  return <Icon {...props}><circle cx="12" cy="12" r="3.2" /><path d="M12 3v5.8M12 15.2V21" /></Icon>
}

export function DiffIcon(props: IconProps) {
  return <Icon {...props}><path d="M12 4.5v7M8.5 8h7M8.5 18.5h7" /></Icon>
}

export function FileIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6.5 3.5h7l4 4v12a1 1 0 0 1-1 1h-10a1 1 0 0 1-1-1v-15a1 1 0 0 1 1-1z" />
      <path d="M13.5 3.5v4h4" />
    </Icon>
  )
}

export function FilesIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8.5 3.5h6l4 4v10a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1v-13a1 1 0 0 1 1-1z" />
      <path d="M14.5 3.5v4h4" />
      <path d="M5 7v12.5A1.5 1.5 0 0 0 6.5 21H15" />
    </Icon>
  )
}

export function FolderIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3.5 7A1.5 1.5 0 0 1 5 5.5h4l2 2h8A1.5 1.5 0 0 1 20.5 9v9a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 18z" />
    </Icon>
  )
}

export function GlobeIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17M12 3.5c2.3 2.4 3.5 5.2 3.5 8.5s-1.2 6.1-3.5 8.5c-2.3-2.4-3.5-5.2-3.5-8.5S9.7 5.9 12 3.5z" />
    </Icon>
  )
}

/** Tile tabs into a grid of panes. */
export function GridIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="3" width="8" height="8" rx="1" />
      <rect x="13" y="3" width="8" height="8" rx="1" />
      <rect x="3" y="13" width="8" height="8" rx="1" />
      <rect x="13" y="13" width="8" height="8" rx="1" />
    </Icon>
  )
}

export function HelpIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9.2a2.5 2.5 0 0 1 4.9.8c0 1.7-2.4 2-2.4 3.6" />
      <path d="M12 17.2h.01" />
    </Icon>
  )
}

export function HomeIcon(props: IconProps) {
  return <Icon {...props}><path d="M4 10.5 12 4l8 6.5V20a1 1 0 0 1-1 1h-4.5v-6h-5v6H5a1 1 0 0 1-1-1z" /></Icon>
}

export function InfoIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5" />
      <circle cx="12" cy="7.75" r="0.6" fill="currentColor" stroke="none" />
    </Icon>
  )
}

export function IssueIcon(props: IconProps) {
  return <Icon {...props}><circle cx="12" cy="12" r="8.5" /><circle cx="12" cy="12" r="1.5" /></Icon>
}

export function KeyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="7.5" cy="15.5" r="4.5" />
      <path d="m10.7 12.3 8.3-8.3" />
      <path d="m16 5 3 3" />
      <path d="m13 8 2.5 2.5" />
    </Icon>
  )
}

/** Arrows out to the corners: show one pane at full size. */
export function MaximizeIcon(props: IconProps) {
  return <Icon {...props}><path d="M4 9V4h5M4 4l6 6M20 9V4h-5M20 4l-6 6M4 15v5h5M4 20l6-6M20 15v5h-5M20 20l-6-6" /></Icon>
}

export function MenuIcon(props: IconProps) {
  return <Icon {...props}><path d="M4 6h16M4 12h16M4 18h16" /></Icon>
}

export function MergeIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="7" cy="5.5" r="2" />
      <circle cx="7" cy="18.5" r="2" />
      <circle cx="17" cy="14" r="2" />
      <path d="M7 7.5v9" />
      <path d="M7 7.5c0 4 4 6.5 8 6.5" />
    </Icon>
  )
}

export function MoonIcon(props: IconProps) {
  return <Icon {...props}><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" /></Icon>
}

export function MoreIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="6" cy="12" r="0.9" />
      <circle cx="12" cy="12" r="0.9" />
      <circle cx="18" cy="12" r="0.9" />
    </Icon>
  )
}

export function NewChatIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
      <path d="M12 7v6M9 10h6" />
    </Icon>
  )
}

export function PinIcon(props: IconProps) {
  return <Icon {...props}><path d="m12 4 2.3 4.8 5.2.7-3.8 3.6.9 5.2L12 15.8l-4.6 2.5.9-5.2-3.8-3.6 5.2-.7z" /></Icon>
}

export function PlusIcon(props: IconProps) {
  return <Icon {...props}><path d="M12 5.5v13M5.5 12h13" /></Icon>
}

/** Arrows in to the centre: return a maximized pane to the grid. */
export function RestoreIcon(props: IconProps) {
  return <Icon {...props}><path d="M10 4v6H4M10 10L4 4M14 4v6h6M14 10l6-6M10 20v-6H4M10 14l-6 6M14 20v-6h6M14 14l6 6" /></Icon>
}

export function RocketIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M5 15c-1.5 1.5-2 5-2 5s3.5-.5 5-2" />
      <path d="M13.5 4.5C16 2 20 2 22 2s0 4-2.5 6.5L14 14l-4-4 3.5-5.5Z" />
      <path d="M10 10l-4 1 3 3 1-4Z" />
      <circle cx="15.5" cy="8.5" r="1.2" />
    </Icon>
  )
}

export function ScheduleIcon(props: IconProps) {
  return <Icon {...props}><circle cx="12" cy="12" r="8.5" /><path d="M12 7.5V12l3 2" /></Icon>
}

export function SearchIcon(props: IconProps) {
  return <Icon {...props}><circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.4-4.4" /></Icon>
}

export function SendIcon(props: IconProps) {
  return <Icon {...props}><path d="M12 19V5M12 5l-6 6M12 5l6 6" /></Icon>
}

export function SettingsIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1Z" />
    </Icon>
  )
}

export function ShieldIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 3 5 6v5c0 4.2 2.8 7.6 7 9 4.2-1.4 7-4.8 7-9V6l-7-3Z" />
      <path d="m9.2 12 1.9 1.9 3.7-3.8" />
    </Icon>
  )
}

/** A frame with its sidebar edge on the right: hide the sidebar. */
export function SidebarCollapseIcon(props: IconProps) {
  return <Icon {...props}><rect x="3" y="4.5" width="18" height="15" rx="2.5" /><path d="M15.5 4.5v15" /></Icon>
}

/** A frame with its sidebar edge on the left: show, or toggle, the sidebar. */
export function SidebarExpandIcon(props: IconProps) {
  return <Icon {...props}><rect x="3" y="4.5" width="18" height="15" rx="2.5" /><path d="M8.5 4.5v15" /></Icon>
}

export function SignOutIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <path d="m16 17 5-5-5-5M21 12H9" />
    </Icon>
  )
}

export function SourceControlIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="7" cy="5.5" r="2" />
      <circle cx="7" cy="18.5" r="2" />
      <circle cx="17" cy="8" r="2" />
      <path d="M7 7.5v9" />
      <path d="M17 10c0 4-4 4.5-10 6.5" />
    </Icon>
  )
}

/** A rotating arc; the caller's CSS animates it. */
export function SpinnerIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="9" opacity="0.25" />
      <path d="M21 12a9 9 0 0 0-9-9" />
    </Icon>
  )
}

/** A frame divided top from bottom: split a pane downward. */
export function SplitDownIcon(props: IconProps) {
  return <Icon {...props}><rect x="3" y="3" width="18" height="18" rx="1" /><path d="M3 12h18" /></Icon>
}

/** A frame divided left from right: split a pane rightward, or collapse a grid back into one. */
export function SplitRightIcon(props: IconProps) {
  return <Icon {...props}><rect x="3" y="3" width="18" height="18" rx="1" /><path d="M12 3v18" /></Icon>
}

export function StopIcon(props: IconProps) {
  return <Icon {...props}><rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor" stroke="none" /></Icon>
}

export function StorefrontIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 9.5 4.5 4h15L21 9.5" />
      <path d="M3 9.5a2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 3 0" />
      <path d="M5 11v8a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-8" />
      <path d="M9 20v-5h6v5" />
    </Icon>
  )
}

export function SunIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </Icon>
  )
}

export function TerminalIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="4.5" width="18" height="15" rx="2" />
      <path d="m7 9.5 3 2.5-3 2.5M12.5 15H17" />
    </Icon>
  )
}

export function ToolboxIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
    </Icon>
  )
}

export function UsageIcon(props: IconProps) {
  return <Icon {...props}><path d="M3 3v18h18M7 16v-5M12 16v-9M17 16v-2" /></Icon>
}

export function WorkflowIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="4" width="7" height="5" rx="1.5" />
      <rect x="14" y="4" width="7" height="5" rx="1.5" />
      <rect x="8.5" y="15" width="7" height="5" rx="1.5" />
      <path d="M10 6.5h4" />
      <path d="M17.5 9v2a2 2 0 0 1-2 2h-7a2 2 0 0 1-2-2V9" />
    </Icon>
  )
}
