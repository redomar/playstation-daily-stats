# PlayStation Network Game Stats Collector

A monorepo project that automatically collects and visualizes your PlayStation Network game library statistics daily at 6:00 AM.

## Overview

This application consists of:
- **Backend (Go)**: Authenticates with PSN, fetches game data, and serves it via REST API
- **Frontend (React + Vite)**: Displays interactive visualizations of your game library

## Features

- Automated daily data collection at 6:00 AM
- Complete game library tracking (up to 600 titles)
- Interactive charts showing playtime distribution
- Game filtering and sorting capabilities
- Responsive design with modern UI components
- Token caching and automatic refresh
- Docker-based deployment

## Tech Stack

### Backend
- Go 1.23
- Docker support
- RESTful API

### Frontend
- React 18
- Vite 7
- TypeScript 5
- Tailwind CSS
- Recharts (data visualization)
- Radix UI (accessible components)

## Project Structure

```
psn/
├── packages/
│   ├── backend/          # Go backend service
│   │   ├── api.go
│   │   ├── server.go
│   │   ├── main.go
│   │   ├── go.mod
│   │   └── Dockerfile
│   └── frontend/         # React frontend
│       ├── src/
│       ├── package.json
│       └── Dockerfile
├── output/               # Generated data files
├── .env                  # Environment variables (root only)
├── docker-compose.yml    # Container orchestration
├── package.json          # Root workspace config
├── CONFIGURATION.md      # Configuration guide
└── README.md
```

**Note**: This monorepo uses **centralized configuration**. All environment variables are managed from the root `.env` file. See [CONFIGURATION.md](CONFIGURATION.md) for details.

## Getting Started

### Prerequisites

- Go 1.23 or higher
- Node.js 22 or higher
- Docker and Docker Compose (for containerized deployment)
- PlayStation Network NPSSO token

### Getting Your NPSSO Token

1. Log into [PlayStation.com](https://www.playstation.com)
2. Open browser developer tools (F12)
3. Go to Application/Storage > Cookies
4. Find the cookie named `npsso`
5. Copy its value

### Environment Variables

**Centralized Configuration**: All environment variables are managed from the root `.env` file. Docker Compose automatically passes these to the containers, and local development reads from the root.

Create a `.env` file in the **root directory** (not in packages):

```env
NPSSO=your_npsso_token_here
ALLOWED_ORIGINS=http://localhost:5173,http://localhost:5173
VITE_ALLOWED_ORIGINS=http://localhost:8080
```

**Important**:
- Keep your NPSSO token secret and never commit it to version control
- Only one `.env` file at root - packages don't need their own
- Use `.env.example` as a template

## Installation & Usage

### Option 1: Docker (Recommended)

```bash
# Clone the repository
git clone https://github.com/redomar/playstation-daily-stats.git
cd psn

# Create .env file with your credentials
cp .env.example .env
# Edit .env with your NPSSO token

# Start services
npm run docker:up

# Stop services
npm run docker:down
```

Services will be available at:
- Frontend: http://localhost:5173
- Backend API: http://localhost:8080

### Option 2: Local Development

```bash
# Install root dependencies
npm install

# Backend
cd packages/backend
go mod tidy
go run .

# Frontend (in a new terminal)
cd packages/frontend
npm install
npm run dev
```

## API Endpoints

### GET /api/latest-output
Returns the most recent snapshot of your game library.

**Response:**
```json
{
  "titles": [...],
  "filename": "output_1234567890.json",
  "timestamp": "1234567890"
}
```

## Configuration

### Schedule Time
Data is collected daily at 6:00 AM (local time). To change this, modify the `scheduledFetch` function in `packages/backend/api.go:37`.

### Data Collection Limits
The backend fetches up to 600 titles (3 requests × 200 titles). To adjust, modify the `offsets` array in `packages/backend/api.go:60`.

## Development

### Backend Development

```bash
cd packages/backend
go run .                    # Run in API mode
go run . -server-only       # Run server without data fetching
go build -o main .          # Build binary
```

### Frontend Development

```bash
cd packages/frontend
npm run dev                 # Start dev server
npm run build              # Production build
npm run lint               # Run linter
```

### Root Scripts

```bash
npm run dev:frontend       # Start frontend dev server
npm run dev:backend        # Start backend
npm run build:frontend     # Build frontend
npm run build:backend      # Build backend
npm run docker:up          # Start Docker containers
npm run docker:down        # Stop Docker containers
```

## Data Storage

Game library snapshots are saved to `output/output_<timestamp>.json` with the following structure:

```json
{
  "titles": [
    {
      "titleId": "...",
      "name": "...",
      "image": "...",
      "category": "...",
      "playDuration": "PT123H45M"
    }
  ]
}
```

## Troubleshooting

### NPSSO Token Invalid
- Tokens expire periodically
- Generate a new token from PlayStation.com
- Update your `.env` file

### Port Already in Use
- Change ports in `docker-compose.yml`
- Update `ALLOWED_ORIGINS` and `VITE_ALLOWED_ORIGINS` accordingly

### No Data Appearing
- Check backend logs for authentication errors
- Verify NPSSO token is valid
- Ensure output directory has write permissions

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE.md) file for details.

## Acknowledgments

- PlayStation Network API documentation
- Community contributors

## Support

For issues, questions, or contributions, please open an issue on GitHub.
