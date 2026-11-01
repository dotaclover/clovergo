import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  base: "/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../web/dist",
    emptyOutDir: true,
    assetsDir: "assets",
    assetsInlineLimit: 0,
    sourcemap: false,
  },
  server: {
    proxy: {
      // The Go service rejects state-changing requests whose Origin differs from
      // its own Host (CSRF guard). The dev server runs on another port, so rewrite
      // Origin to the backend's address; this only affects the local dev proxy.
      "/api": {
        target: "http://127.0.0.1:8088",
        changeOrigin: true,
        headers: { origin: "http://127.0.0.1:8088" },
      },
      "/health": { target: "http://127.0.0.1:8088", changeOrigin: true },
    },
  },
});
