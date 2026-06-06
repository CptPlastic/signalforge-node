import type { Config } from 'tailwindcss'

/** Hub console — CSS var palettes per display mode. See signalforge.org/DISPLAY-MODES.md */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        console: {
          bg: 'var(--sf-bg)',
          surface: 'var(--sf-surface)',
          border: 'var(--sf-border)',
          text: 'var(--sf-text)',
          muted: 'var(--sf-muted)',
          accent: 'var(--sf-accent)',
          amber: 'var(--sf-accent-bright)',
          error: 'var(--sf-error)',
        },
      },
    },
  },
  plugins: [],
} satisfies Config
