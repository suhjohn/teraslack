import { cn } from "#/lib/utils"

type SkeletonProps = React.HTMLAttributes<HTMLElement> & {
  as?: 'div' | 'span'
}

function Skeleton({
  as: Component = 'div',
  className,
  ...props
}: SkeletonProps) {
  return (
    <Component
      data-slot="skeleton"
      className={cn("animate-pulse rounded-md bg-muted", className)}
      {...props}
    />
  )
}

export { Skeleton }
