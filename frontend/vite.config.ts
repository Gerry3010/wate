/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import wails from "@wailsio/runtime/plugins/vite";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [wails("./bindings")],
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
    // Without this vitest hands every CSS import back as an empty string, and the test that
    // holds the stylesheets to stepped animations would pass by reading nothing.
    css: true,
  },
});
