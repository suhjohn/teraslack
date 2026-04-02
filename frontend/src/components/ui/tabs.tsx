import type { ButtonHTMLAttributes, HTMLAttributes } from 'react'
import {
  createContext,
  useContext,
  useId,
  useState,
} from 'react'
import { cn } from '../../lib/utils'

/* ------------------------------------------------------------------ */
/*  Context                                                           */
/* ------------------------------------------------------------------ */

type TabsContextValue = {
  value: string
  onValueChange: (value: string) => void
  baseId: string
}

const TabsContext = createContext<TabsContextValue | null>(null)

function useTabsContext() {
  const ctx = useContext(TabsContext)
  if (!ctx) throw new Error('Tabs compound components must be used within <Tabs>')
  return ctx
}

/* ------------------------------------------------------------------ */
/*  Tabs (root)                                                       */
/* ------------------------------------------------------------------ */

type TabsProps = HTMLAttributes<HTMLDivElement> & {
  value?: string
  defaultValue?: string
  onValueChange?: (value: string) => void
}

export function Tabs({
  value: controlledValue,
  defaultValue = '',
  onValueChange,
  className,
  ...props
}: TabsProps) {
  const [uncontrolledValue, setUncontrolledValue] = useState(defaultValue)
  const baseId = useId()

  const value = controlledValue ?? uncontrolledValue
  const setValue = onValueChange ?? setUncontrolledValue

  return (
    <TabsContext.Provider value={{ value, onValueChange: setValue, baseId }}>
      <div className={cn(className)} {...props} />
    </TabsContext.Provider>
  )
}

/* ------------------------------------------------------------------ */
/*  TabsList                                                          */
/* ------------------------------------------------------------------ */

export function TabsList({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      role="tablist"
      className={cn(
        'flex flex-wrap gap-2 border-b border-[var(--sys-home-border)] pb-2',
        className,
      )}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  TabsTrigger                                                       */
/* ------------------------------------------------------------------ */

type TabsTriggerProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  value: string
}

export function TabsTrigger({
  value,
  className,
  ...props
}: TabsTriggerProps) {
  const { value: activeValue, onValueChange, baseId } = useTabsContext()
  const isActive = value === activeValue

  return (
    <button
      role="tab"
      type="button"
      id={`${baseId}-trigger-${value}`}
      aria-selected={isActive}
      aria-controls={`${baseId}-content-${value}`}
      data-status={isActive ? 'active' : 'inactive'}
      className={cn(
        'border px-3 py-1.5 font-[family-name:var(--font-mono)] text-[10px] font-bold uppercase tracking-[0.08em] transition-colors',
        isActive
          ? 'border-[var(--sys-home-border)] bg-[var(--sys-home-accent-bg)] text-[var(--sys-home-accent-fg)]'
          : 'border-[var(--sys-home-border)] text-[var(--sys-home-muted)] hover:bg-[var(--sys-home-bg)] hover:text-[var(--sys-home-fg)]',
        className,
      )}
      onClick={() => onValueChange(value)}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  TabsContent                                                       */
/* ------------------------------------------------------------------ */

type TabsContentProps = HTMLAttributes<HTMLDivElement> & {
  value: string
}

export function TabsContent({
  value,
  className,
  ...props
}: TabsContentProps) {
  const { value: activeValue, baseId } = useTabsContext()
  if (value !== activeValue) return null

  return (
    <div
      role="tabpanel"
      id={`${baseId}-content-${value}`}
      aria-labelledby={`${baseId}-trigger-${value}`}
      className={cn(className)}
      {...props}
    />
  )
}
