import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./test/e2e",
  workers: 1,
  use: { baseURL: process.env.TASKFLOW_FLOW_BASE_URL, ...devices["Desktop Chrome"], trace: "off" },
});
