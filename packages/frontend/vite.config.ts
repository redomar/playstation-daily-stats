import path from "path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
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
