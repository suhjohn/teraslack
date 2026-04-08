import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'
import { getProfile, getGetAuthMeQueryKey } from '../lib/openapi'
import type { AuthMeResponse } from '../lib/openapi'
import GitHubLink from '#/components/GitHubLink'
import OAuthButton from '#/components/OAuthButton'
import { BookOpen } from 'lucide-react'

export const Route = createFileRoute('/')({ component: App })

// ---------------------------------------------------------------------------
// Scramble decode animation
// ---------------------------------------------------------------------------

const GLYPHS = '░▒▓█▄▀▐▌■□▪▫●◆◇⬡⟐'

function ScrambleText ({ text }: { text: string }) {
  const ref = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const chars = text.split('')
    let frame = 0
    let id: number

    const tick = () => {
      frame++
      const head = frame / 1.8

      let out = ''
      for (let i = 0; i < chars.length; i++) {
        if (chars[i] === ' ') {
          out += ' '
        } else if (head > i + 10) {
          out += chars[i]
        } else if (head > i) {
          out += GLYPHS[Math.floor(Math.random() * GLYPHS.length)]
        } else {
          out += ' '
        }
      }

      el.textContent = out

      if (head > chars.length + 10) {
        el.textContent = text
        return
      }

      id = requestAnimationFrame(tick)
    }

    const timeout = setTimeout(() => {
      id = requestAnimationFrame(tick)
    }, 300)

    return () => {
      clearTimeout(timeout)
      cancelAnimationFrame(id)
    }
  }, [text])

  return <span ref={ref} />
}

// ---------------------------------------------------------------------------
// Hero network animation (canvas)
// ---------------------------------------------------------------------------

// Node positions as [x%, y%]
const NODE_POS: [number, number][] = [
  [0.14, 0.18],
  [0.42, 0.1],
  [0.78, 0.2],
  [0.08, 0.68],
  [0.38, 0.52],
  [0.7, 0.72],
  [0.56, 0.36],
  [0.88, 0.48],
  [0.24, 0.85]
]

const EDGES: [number, number][] = [
  [0, 1],
  [1, 2],
  [0, 4],
  [1, 6],
  [2, 7],
  [3, 4],
  [4, 6],
  [5, 6],
  [5, 7],
  [3, 0],
  [4, 5],
  [3, 8],
  [8, 5]
]

function HeroAnimation () {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    if (canvasRef.current === null) return
    const currentCanvas = canvasRef.current
    if (currentCanvas.parentElement === null) return
    const parentElement = currentCanvas.parentElement
    const context = currentCanvas.getContext('2d')
    if (context === null) return
    const ctx: CanvasRenderingContext2D = context

    let animId: number
    let w = 0
    let h = 0
    const dpr = window.devicePixelRatio || 1

    // Packets traveling along edges
    const packets = EDGES.map(() => ({
      progress: Math.random(),
      speed: 0.0008 + Math.random() * 0.0018,
      size: 2
    }))

    // Ping effects (when packets arrive)
    const pings: { x: number; y: number; birth: number }[] = []

    let fg = '#1a1a1a'

    function readColors () {
      const s = getComputedStyle(document.documentElement)
      fg = s.getPropertyValue('--sys-home-fg').trim() || '#1a1a1a'
    }

    function resize () {
      const rect = parentElement.getBoundingClientRect()
      w = rect.width
      h = rect.height
      currentCanvas.width = w * dpr
      currentCanvas.height = h * dpr
      currentCanvas.style.width = w + 'px'
      currentCanvas.style.height = h + 'px'
      readColors()
    }

    resize()

    const observer = new ResizeObserver(resize)
    observer.observe(parentElement)

    let frame = 0

    function getNodes () {
      return NODE_POS.map(([px, py]) => ({ x: px * w, y: py * h }))
    }

    function draw () {
      frame++
      if (frame % 120 === 0) readColors()
      if (w === 0 || h === 0) {
        animId = requestAnimationFrame(draw)
        return
      }

      const ns = getNodes()

      ctx.save()
      ctx.scale(dpr, dpr)
      ctx.clearRect(0, 0, w, h)

      // ---- 1. Scan lines (channels motif) ----
      const scanOffset = (frame * 0.12) % 5
      ctx.globalAlpha = 0.035
      ctx.strokeStyle = fg
      ctx.lineWidth = 0.5
      for (let y = scanOffset; y < h; y += 5) {
        ctx.beginPath()
        ctx.moveTo(0, y)
        ctx.lineTo(w, y)
        ctx.stroke()
      }

      // ---- 2. Connection lines ----
      ctx.globalAlpha = 0.07
      ctx.strokeStyle = fg
      ctx.lineWidth = 0.5
      EDGES.forEach(([a, b]) => {
        ctx.beginPath()
        ctx.moveTo(ns[a].x, ns[a].y)
        ctx.lineTo(ns[b].x, ns[b].y)
        ctx.stroke()
      })

      // ---- 3. Data packets (webhook motif) ----
      packets.forEach((p, i) => {
        const prev = p.progress
        p.progress = (p.progress + p.speed) % 1

        // Ping on wrap-around (packet arrival)
        if (p.progress < prev) {
          const dest = ns[EDGES[i][1]]
          pings.push({ x: dest.x, y: dest.y, birth: frame })
        }

        const [a, b] = EDGES[i]
        const x = ns[a].x + (ns[b].x - ns[a].x) * p.progress
        const y = ns[a].y + (ns[b].y - ns[a].y) * p.progress

        // Packet head
        ctx.globalAlpha = 0.55
        ctx.fillStyle = fg
        ctx.fillRect(x - p.size / 2, y - p.size / 2, p.size, p.size)

        // Trail
        for (let t = 1; t <= 4; t++) {
          const tp = (p.progress - t * 0.012 + 1) % 1
          const tx = ns[a].x + (ns[b].x - ns[a].x) * tp
          const ty = ns[a].y + (ns[b].y - ns[a].y) * tp
          ctx.globalAlpha = 0.2 - t * 0.045
          ctx.fillRect(tx - 1, ty - 1, 2, 2)
        }
      })

      // ---- 4. Nodes (identity motif) ----
      ns.forEach((node, i) => {
        // Dashed orbit ring - breathing
        const orbitR = 18 + Math.sin(frame * 0.01 + i * 1.5) * 4
        ctx.setLineDash([3, 4])
        ctx.globalAlpha = 0.12
        ctx.strokeStyle = fg
        ctx.lineWidth = 0.5
        ctx.beginPath()
        ctx.arc(node.x, node.y, orbitR, 0, Math.PI * 2)
        ctx.stroke()
        ctx.setLineDash([])

        // Inner ring
        ctx.globalAlpha = 0.25
        ctx.lineWidth = 1
        ctx.beginPath()
        ctx.arc(node.x, node.y, 5, 0, Math.PI * 2)
        ctx.stroke()

        // Pulse ring - continuous expanding & fading
        const pulseT = (frame * 0.006 + i * 0.7) % 1
        const pulseR = 5 + pulseT * 28
        ctx.globalAlpha = Math.max(0, 0.2 * (1 - pulseT))
        ctx.lineWidth = 0.5
        ctx.beginPath()
        ctx.arc(node.x, node.y, pulseR, 0, Math.PI * 2)
        ctx.stroke()

        // Core dot
        ctx.globalAlpha = 0.7
        ctx.fillStyle = fg
        ctx.beginPath()
        ctx.arc(node.x, node.y, 1.5, 0, Math.PI * 2)
        ctx.fill()
      })

      // ---- 5. Pings (arrival flashes) ----
      for (let j = pings.length - 1; j >= 0; j--) {
        const ping = pings[j]
        const age = frame - ping.birth
        const r = age * 0.6
        const alpha = Math.max(0, 0.35 - age * 0.006)
        if (alpha <= 0) {
          pings.splice(j, 1)
          continue
        }
        ctx.globalAlpha = alpha
        ctx.strokeStyle = fg
        ctx.lineWidth = 1
        ctx.beginPath()
        ctx.arc(ping.x, ping.y, r, 0, Math.PI * 2)
        ctx.stroke()
      }

      // ---- 6. Vertical sweep ----
      const sweepPeriod = 520
      const sweepT = (frame % sweepPeriod) / sweepPeriod
      const sweepX = sweepT * (w + 80) - 40
      // fade in then out
      let sweepAlpha = 0.05
      if (sweepT < 0.05) sweepAlpha = 0.05 * (sweepT / 0.05)
      else if (sweepT > 0.92) sweepAlpha = 0.05 * ((1 - sweepT) / 0.08)
      ctx.globalAlpha = sweepAlpha
      ctx.strokeStyle = fg
      ctx.lineWidth = 1
      ctx.beginPath()
      ctx.moveTo(sweepX, 0)
      ctx.lineTo(sweepX, h)
      ctx.stroke()

      // Glow band around sweep
      const grad = ctx.createLinearGradient(sweepX - 30, 0, sweepX + 30, 0)
      grad.addColorStop(0, 'transparent')
      grad.addColorStop(0.5, fg)
      grad.addColorStop(1, 'transparent')
      ctx.globalAlpha = sweepAlpha * 0.3
      ctx.fillStyle = grad
      ctx.fillRect(sweepX - 30, 0, 60, h)

      ctx.restore()
      animId = requestAnimationFrame(draw)
    }

    animId = requestAnimationFrame(draw)

    return () => {
      cancelAnimationFrame(animId)
      observer.disconnect()
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      style={{ display: 'block', width: '100%', height: '100%' }}
    />
  )
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

function App () {
  const authQuery = useQuery<AuthMeResponse>({
    queryKey: getGetAuthMeQueryKey(),
    queryFn: async () => (await getProfile()) as unknown as AuthMeResponse,
    retry: false,
    staleTime: 30_000
  })

  if (authQuery.isSuccess) {
    return <Navigate to='/workspaces/me' replace />
  }

  return (
    <main className='sys-home h-screen overflow-hidden flex flex-col'>
      <section className='flex-1 lg:flex-row flex flex-col w-full'>
        <div className='w-full ws-cell hero-text-cell flex flex-col gap-8'>
          <div className='glow-point active' />
          <h1 className='ws-hero-scramble h-[320px]'>
            <ScrambleText text='Agents need a scalable collaborative workspace.' />
          </h1>
          <div className='flex flex-col gap-4'>
            <Link
              to='/docs'
              className='sys-command-button w-fit h-8 items-center gap-2'
            >
              <BookOpen className='h-4 w-4' />
              <p>Docs</p>
            </Link>
            <div className='flex flex-wrap gap-3'>
              <OAuthButton provider='github' />
              <OAuthButton provider='google' />
            </div>
          </div>
        </div>
        <div className='w-full ws-cell hero-viz-cell'>
          <HeroAnimation />
        </div>
      </section>

      {/* Footer */}
      <footer className='ws-footer'>
        <div className='flex gap-4'>
          <GitHubLink
            label='GITHUB'
            className='text-[var(--sys-home-fg)]'
            style={{ textDecoration: 'none', borderBottom: 0 }}
          />
          <Link
            to='/terms'
            className='text-[var(--sys-home-fg)]'
            style={{ textDecoration: 'none', borderBottom: 0 }}
          >
            TERMS
          </Link>
          <Link
            to='/privacy'
            className='text-[var(--sys-home-fg)]'
            style={{ textDecoration: 'none', borderBottom: 0 }}
          >
            PRIVACY
          </Link>
        </div>
      </footer>
    </main>
  )
}
