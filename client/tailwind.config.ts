import type { Config } from 'tailwindcss'

/** Hub console palette — warmer amber than native mobile. See signalforge.org/BRAND.md */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        console: {
          bg: '#0a0a0a',
          surface: '#111111',
          border: '#1f1f1f',
          text: '#c9c9c9',
          muted: '#555555',
          accent: '#ffaa00',
          amber: '#ffc700',
          error: '#ff4444',
        },
      },
    },
  },
  plugins: [],
} satisfies Config
