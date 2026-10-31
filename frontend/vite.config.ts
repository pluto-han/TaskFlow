import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    plugins: [
      react(),
      {
        name: "production-security-policy",
        apply: "build",
        transformIndexHtml() {
          return [
            {
              tag: "meta",
              attrs: {
                "http-equiv": "Content-Security-Policy",
                content:
                  "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'",
              },
              injectTo: "head-prepend",
            },
          ];
        },
      },
    ],
    server: {
      port: 5173,
      strictPort: true,
      proxy: {
        "/api": {
          target: env.TASKFLOW_API_TARGET || "http://127.0.0.1:9090",
          changeOrigin: false,
          rewrite: (path) => path.replace(/^\/api/, ""),
        },
      },
    },
  };
});
