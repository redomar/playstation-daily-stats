/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        editorial: ['"Playfair Display"', 'Georgia', 'serif'],
        mono: ['"JetBrains Mono"', 'monospace'],
        body: ['"DM Sans"', 'system-ui', 'sans-serif'],
      },
      colors: {
        paper: '#FAFAF8',
        ink: '#1A1A1A',
        'editorial-red': '#E63312',
        'data-blue': '#0055FF',
        muted: '#999999',
        'row-alt': '#F5F5F3',
        'rule': '#E0E0DC',
      },
      borderRadius: {
        DEFAULT: '2px',
        sm: '1px',
        md: '2px',
        lg: '2px',
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
}
