# PSN Frontend

React + Vite frontend application for visualizing PlayStation Network game statistics.

## Features

- Game library overview with statistics
- Interactive charts showing playtime distribution
- Filtering and sorting capabilities
- Responsive design with Tailwind CSS
- Radix UI components

## Tech Stack

- React 18
- Vite 7
- TypeScript 5
- Tailwind CSS
- Recharts for data visualization
- Radix UI for accessible components

## Environment Variables

- `VITE_ALLOWED_ORIGINS`: API endpoint URL (default: http://localhost:8080)

## Running Locally

```bash
npm install
npm run dev
```

## Building

```bash
npm run build
npm run preview
```

## Docker

```bash
docker build -t psn-frontend .
docker run -p 5173:5173 psn-frontend
```
