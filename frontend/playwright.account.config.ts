import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./test/e2e",
  testMatch: "account.spec.ts",
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:5187",
    ...devices["Desktop Chrome"],
    trace: "off",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: "npm run preview -- --port 5187 --strictPort",
    url: "http://127.0.0.1:5187",
    reuseExistingServer: false,
  },
});
