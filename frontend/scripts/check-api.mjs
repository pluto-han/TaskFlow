import { readFile } from "node:fs/promises";
import openapiTS, { astToString } from "openapi-typescript";

const generated = astToString(
  await openapiTS(new URL("../../api/openapi.json", import.meta.url)),
);
const checkedIn = await readFile(
  new URL("../src/api/schema.d.ts", import.meta.url),
  "utf8",
);
if (generated !== checkedIn.replace(/^\/\*\*[\s\S]*?\*\/\n\n/, "")) {
  console.error(
    "API types are stale. Run npm run api:generate and review the diff.",
  );
  process.exitCode = 1;
}
