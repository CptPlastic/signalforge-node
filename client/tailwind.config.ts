import type { Config } from 'tailwindcss'

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
          accent: '#00ff41',
          amber: '#ffb000',
          error: '#ff4444',
        },
      },
    },
  },
  plugins: [],
} satisfies Config
