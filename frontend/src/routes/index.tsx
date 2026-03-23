import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'
import { ArrowRight, Terminal, Lock, Layers } from 'lucide-react'

export const Route = createFileRoute('/')({ component: App })

function DotGrid() {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const gap = 24
    let animFrame: number

    function resize() {
      const dpr = window.devicePixelRatio || 1
      canvas!.width = canvas!.offsetWidth * dpr
      canvas!.height = canvas!.offsetHeight * dpr
      ctx!.scale(dpr, dpr)
    }

    resize()
    window.addEventListener('resize', resize)

    function draw(time: number) {
      const w = canvas!.offsetWidth
      const h = canvas!.offsetHeight
      ctx!.clearRect(0, 0, w, h)

      const cols = Math.floor(w / gap)
      const rows = Math.floor(h / gap)
      const offsetX = (w - cols * gap) / 2
      const offsetY = (h - rows * gap) / 2

      const isDark =
        document.documentElement.classList.contains('dark')

      for (let r = 0; r < rows; r++) {
        for (let c = 0; c < cols; c++) {
          const x = offsetX + c * gap + gap / 2
          const y = offsetY + r * gap + gap / 2

          const wave =
            Math.sin(time * 0.001 + c * 0.3 + r * 0.2) * 0.5 + 0.5
          const opacity = 0.06 + wave * 0.2

          ctx!.beginPath()
          ctx!.arc(x, y, 1.2, 0, Math.PI * 2)
          ctx!.fillStyle = isDark
            ? `rgba(255,255,255,${opacity})`
            : `rgba(0,0,0,${opacity})`
          ctx!.fill()
        }
      }

      animFrame = requestAnimationFrame(draw)
    }

    animFrame = requestAnimationFrame(draw)

    return () => {
      cancelAnimationFrame(animFrame)
      window.removeEventListener('resize', resize)
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      className="pointer-events-none absolute inset-0 h-full w-full"
      style={{ zIndex: 0 }}
    />
  )
}

function App() {
  return (
    <main className="page-wrap px-4 pb-16 pt-16">
      {/* hero */}
      <section className="rise-in relative overflow-hidden border border-[var(--line)] bg-[var(--surface-strong)] p-8 sm:p-12">
        <DotGrid />
        <div className="relative z-10">
          <p className="eyebrow mb-4">Teraslack</p>
          <h1 className="display-title mb-6 max-w-3xl text-3xl leading-[1.1] text-[var(--ink)] sm:text-5xl">
            Infrastructure for team messaging with explicit access control.
          </h1>
          <p className="mb-8 max-w-xl text-sm leading-relaxed text-[var(--ink-soft)]">
            API on <code>api.teraslack.ai</code>. Frontend on TanStack Start.
            Admin workflows for teams, users, and delegated roles.
          </p>
          <div className="flex flex-wrap gap-3">
            <a href="/login" className="action-button no-underline border-b-0">
              Login
              <ArrowRight className="h-3.5 w-3.5" />
            </a>
            <a
              href="/admin"
              className="secondary-button no-underline border-b-0"
            >
              Admin console
            </a>
          </div>
        </div>
      </section>

      {/* features */}
      <section className="mt-6 grid gap-px border border-[var(--line)] bg-[var(--line)] sm:grid-cols-3">
        {[
          {
            icon: <Terminal className="h-4 w-4" />,
            title: 'Workspace controls',
            desc: 'Inspect teams, domains, and workspace config from a single surface.',
          },
          {
            icon: <Lock className="h-4 w-4" />,
            title: 'Access management',
            desc: 'Promote admins, edit roles, keep access changes visible.',
          },
          {
            icon: <Layers className="h-4 w-4" />,
            title: 'Frontend + API split',
            desc: 'Dedicated frontend on teraslack.ai, API on api.teraslack.ai.',
          },
        ].map((item, i) => (
          <article
            key={item.title}
            className="rise-in bg-[var(--surface-strong)] p-6"
            style={{ animationDelay: `${i * 80 + 100}ms` }}
          >
            <div className="mb-3 inline-flex h-8 w-8 items-center justify-center border border-[var(--line)] text-[var(--ink)]">
              {item.icon}
            </div>
            <h2 className="mb-1.5 text-sm font-bold text-[var(--ink)]">
              {item.title}
            </h2>
            <p className="m-0 text-xs leading-relaxed text-[var(--ink-soft)]">
              {item.desc}
            </p>
          </article>
        ))}
      </section>

      {/* details */}
      <section className="mt-6 border border-[var(--line)] bg-[var(--surface-strong)] p-6 rise-in" style={{ animationDelay: '350ms' }}>
        <p className="eyebrow mb-3">Implementation</p>
        <ul className="m-0 list-none space-y-2 pl-0 text-xs leading-relaxed text-[var(--ink-soft)]">
          <li className="flex gap-2">
            <span className="text-[var(--ink)]">--</span>
            OAuth handoff through the API with redirect back to frontend.
          </li>
          <li className="flex gap-2">
            <span className="text-[var(--ink)]">--</span>
            Session-aware dashboard backed by <code>/auth/me</code>,{' '}
            <code>/teams</code>, <code>/users</code>.
          </li>
          <li className="flex gap-2">
            <span className="text-[var(--ink)]">--</span>
            CORS and secure cookie handling for cross-origin requests.
          </li>
        </ul>
      </section>
    </main>
  )
}
