import type { HTMLAttributes, ReactNode } from 'react'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react'
import { cn } from '../../lib/utils'

/* ------------------------------------------------------------------ */
/*  Context                                                           */
/* ------------------------------------------------------------------ */

type DialogContextValue = {
  open: boolean
  setOpen: (open: boolean) => void
}

const DialogContext = createContext<DialogContextValue | null>(null)

function useDialogContext() {
  const ctx = useContext(DialogContext)
  if (!ctx) throw new Error('Dialog compound components must be used within <Dialog>')
  return ctx
}

/* ------------------------------------------------------------------ */
/*  Dialog (root)                                                     */
/* ------------------------------------------------------------------ */

type DialogProps = {
  open?: boolean
  defaultOpen?: boolean
  onOpenChange?: (open: boolean) => void
  children: ReactNode
}

export function Dialog({
  open: controlledOpen,
  defaultOpen = false,
  onOpenChange,
  children,
}: DialogProps) {
  const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen)

  const open = controlledOpen ?? uncontrolledOpen
  const setOpen = useCallback(
    (next: boolean) => {
      onOpenChange?.(next)
      if (controlledOpen === undefined) setUncontrolledOpen(next)
    },
    [controlledOpen, onOpenChange],
  )

  return (
    <DialogContext.Provider value={{ open, setOpen }}>
      {children}
    </DialogContext.Provider>
  )
}

/* ------------------------------------------------------------------ */
/*  DialogTrigger                                                     */
/* ------------------------------------------------------------------ */

type DialogTriggerProps = HTMLAttributes<HTMLButtonElement> & {
  asChild?: boolean
}

export function DialogTrigger({
  className,
  onClick,
  ...props
}: DialogTriggerProps) {
  const { setOpen } = useDialogContext()

  return (
    <button
      type="button"
      className={cn(className)}
      onClick={(e) => {
        onClick?.(e)
        setOpen(true)
      }}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  DialogContent — uses native <dialog> for accessibility            */
/* ------------------------------------------------------------------ */

type DialogContentProps = HTMLAttributes<HTMLDivElement>

export function DialogContent({
  className,
  children,
  ...props
}: DialogContentProps) {
  const { open, setOpen } = useDialogContext()
  const dialogRef = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const el = dialogRef.current
    if (!el) return

    if (open && !el.open) {
      el.showModal()
    } else if (!open && el.open) {
      el.close()
    }
  }, [open])

  useEffect(() => {
    const el = dialogRef.current
    if (!el) return

    const handleClose = () => setOpen(false)
    el.addEventListener('close', handleClose)
    return () => el.removeEventListener('close', handleClose)
  }, [setOpen])

  return (
    <dialog
      ref={dialogRef}
      className="m-auto max-h-[85vh] w-full max-w-lg border border-[var(--sys-home-border)] bg-[var(--sys-home-bg)] p-0 font-[family-name:var(--font-mono)] text-[var(--sys-home-fg)] backdrop:bg-black/50 open:flex open:flex-col"
      onClick={(e) => {
        // close on backdrop click
        if (e.target === dialogRef.current) setOpen(false)
      }}
    >
      <div className={cn('flex flex-col gap-4 p-6', className)} {...props}>
        {children}
      </div>
    </dialog>
  )
}

/* ------------------------------------------------------------------ */
/*  DialogHeader                                                      */
/* ------------------------------------------------------------------ */

export function DialogHeader({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('space-y-2', className)} {...props} />
}

/* ------------------------------------------------------------------ */
/*  DialogTitle                                                       */
/* ------------------------------------------------------------------ */

export function DialogTitle({
  className,
  ...props
}: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h2
      className={cn(
        'text-base font-bold uppercase tracking-[0.06em] text-[var(--sys-home-fg)]',
        className,
      )}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  DialogDescription                                                 */
/* ------------------------------------------------------------------ */

export function DialogDescription({
  className,
  ...props
}: HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn('text-[12px] text-[var(--sys-home-muted)]', className)}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  DialogFooter                                                      */
/* ------------------------------------------------------------------ */

export function DialogFooter({
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'flex items-center justify-end gap-2 border-t border-[var(--sys-home-border)] pt-4',
        className,
      )}
      {...props}
    />
  )
}

/* ------------------------------------------------------------------ */
/*  DialogClose                                                       */
/* ------------------------------------------------------------------ */

export function DialogClose({
  className,
  onClick,
  ...props
}: HTMLAttributes<HTMLButtonElement>) {
  const { setOpen } = useDialogContext()

  return (
    <button
      type="button"
      className={cn(className)}
      onClick={(e) => {
        onClick?.(e as any)
        setOpen(false)
      }}
      {...props}
    />
  )
}
