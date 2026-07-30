import {
  Bookmark,
  CalendarDays,
  ChartNoAxesGantt,
  Check,
  Command,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  Download,
  ExternalLink,
  FileText,
  Filter,
  History,
  Info,
  ListTree,
  LoaderCircle,
  MessageSquare,
  Network,
  Plus,
  RefreshCw,
  Search,
  Settings,
  Share2,
  ShieldCheck,
  SquareCheckBig,
  Star,
  Trash2,
  TriangleAlert,
  X,
  type LucideIcon,
} from 'lucide-react'

export const ICONS = {
  changes: ListTree,
  graph: Network,
  timeline: ChartNoAxesGantt,
  search: Search,
  command: Command,
  recent: History,
  lint: ShieldCheck,
  calendar: CalendarDays,
  todos: SquareCheckBig,
  report: FileText,
  bookmark: Bookmark,
  settings: Settings,
  open: ExternalLink,
  copy: Copy,
  close: X,
  check: Check,
  star: Star,
  'star-filled': Star,
  refresh: RefreshCw,
  plus: Plus,
  trash: Trash2,
  share: Share2,
  chat: MessageSquare,
  filter: Filter,
  download: Download,
  'chevron-right': ChevronRight,
  'chevron-left': ChevronLeft,
  'chevron-down': ChevronDown,
  warning: TriangleAlert,
  info: Info,
  spinner: LoaderCircle,
} as const satisfies Record<string, LucideIcon>

export type IconName = keyof typeof ICONS

export function isIconName(name: string): name is IconName {
  return Object.prototype.hasOwnProperty.call(ICONS, name)
}

export interface IconProps {
  name: IconName
  size?: number
  className?: string
  label?: string
}

export function Icon({ name, size = 16, className, label }: IconProps) {
  const Glyph = ICONS[name]
  return (
    <Glyph
      size={size}
      strokeWidth={1.5}
      fill={name === 'star-filled' ? 'currentColor' : 'none'}
      className={className}
      aria-hidden={label ? undefined : true}
      role={label ? 'img' : undefined}
      aria-label={label}
      focusable="false"
    />
  )
}
