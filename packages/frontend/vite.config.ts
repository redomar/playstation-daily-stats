import path from "path";
import react from "@vitejs/plugin-react";
import { defineConfig, Plugin } from "vite";

// Block requests for sensitive files (.env, /proc/*, etc.)
function blockSensitiveFiles(): Plugin {
  return {
    name: "block-sensitive-files",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = req.url || "";
        if (
          url.includes(".env") ||
          url.includes("/proc/") ||
          url.includes("environ")
        ) {
          res.statusCode = 404;
          res.end();
          return;
        }
        next();
      });
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [blockSensitiveFiles(), react()],
  server: {
    host: "0.0.0.0",
    allowedHosts: ["psn.rx1.uk", "localhost", ".localhost"],
    watch: {
      ignored: ["!**/output/**"],
    },
  },
  resolve: {
    alias: {
      "@output": path.resolve(__dirname, "./output"),
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
