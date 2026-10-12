import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // Hub на том же origin; токен подставляет прокси из окружения без префикса VITE_ (HUB_TOKEN), во фронт он не попадает.
      "/api/network": {
        target: process.env.HUB_URL ?? "http://localhost:8090",
        headers: process.env.HUB_TOKEN ? { Authorization: `Bearer ${process.env.HUB_TOKEN}` } : {},
      },
      "/api": "http://localhost:8080", "/events": "http://localhost:8080",
    },
  },
});
