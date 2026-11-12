import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 3500,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules/lucide-react")) {
            return "lucide-icons";
          }
          if (id.includes("node_modules/@openuidev")) {
            return "openui-sdk";
          }
          if (id.includes("node_modules")) {
            return "vendor";
          }
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/admin": "http://localhost:8080",
      "/v1": "http://localhost:8080",
      "/auth": "http://localhost:8080",
      "/backend-api": "http://localhost:8080",
    },
  },
});
