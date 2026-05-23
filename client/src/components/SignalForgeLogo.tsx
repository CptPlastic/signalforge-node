type SignalForgeLogoProps = Readonly<{
  className?: string
}>

export function SignalForgeLogo({ className = '' }: SignalForgeLogoProps) {
  return (
    <svg className={className} viewBox="0 0 120 120" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden>
      <path d="M60,15L60,88" stroke="currentColor" strokeWidth="2.5" />
      <path d="M60,36L38,55" stroke="currentColor" strokeWidth="2" />
      <path d="M60,36L82,55" stroke="currentColor" strokeWidth="2" />
      <path d="M60,55L22,80" stroke="currentColor" strokeWidth="2" />
      <path d="M60,55L98,80" stroke="currentColor" strokeWidth="2" />
      <path d="M22,80L98,80" stroke="currentColor" strokeWidth="1.5" />
      <g transform="translate(0,8)"><path d="M46,28C55.333,18.667 64.667,18.667 74,28" stroke="currentColor" strokeWidth="2" /></g>
      <g transform="translate(0,2)"><path d="M46,28C55.333,18.667 64.667,18.667 74,28" stroke="currentColor" strokeWidth="2" /></g>
      <g transform="translate(0,-4)"><path d="M46,28C55.333,18.667 64.667,18.667 74,28" stroke="currentColor" strokeWidth="2" /></g>
    </svg>
  )
}