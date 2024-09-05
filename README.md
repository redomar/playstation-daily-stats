# PS Network Game Stats

This project fetches and displays your PS Network game list, including play time and other statistics.

## Table of Contents

- [Setup](#setup)
- [Usage](#usage)
- [Environment Variables](#environment-variables)
- [Contributing](#contributing)
- [License](#license)

## Setup

1. Clone the repository:
  ```
  git clone https://github.com/redomar/playstation-daily-stats.git
  cd playstation-daily-stats
  ```

2. Install dependencies for both the backend and frontend:
  ```
  # Backend
  cd backend
  go mod tidy

  # Frontend
  cd ../vite-frontend
  npm install
  ```

3. Set up your environment variables (see [Environment Variables](#environment-variables) section).

4. Build and run the backend:
  ```
  cd backend
  go build
  ./psn-game-list-viewer
  ```

5. In a separate terminal, start the frontend development server:
  ```
  cd vite-frontend
  npm run dev
  ```

## Usage

1. Ensure both the backend and frontend are running.
2. Open your browser and navigate to `http://localhost:5173` (or the port specified by Vite).
3. You should see your PlayStation Network game list displayed, including play time and other statistics.

## Environment Variables

Create a `.env` file in the root of the backend directory with the following content:

```
NPSSO=your_npsso_token_here
```
**Note:** Keep your NPSSO token secret and do not share it with others.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
