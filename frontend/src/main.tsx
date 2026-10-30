import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { App } from "./App";
import "./styles.css";

// Capture fragments before the router mounts, then remove them from history.
// Never store invitation credentials in query strings or browser storage.
const fragment =
  window.location.pathname === "/invite"
    ? new URLSearchParams(window.location.hash.slice(1)).get("token") || ""
    : "";
const invitationToken = /^[A-Za-z0-9_-]{43}$/.test(fragment) ? fragment : "";
if (window.location.pathname === "/invite")
  window.history.replaceState(
    window.history.state,
    "",
    window.location.pathname + window.location.search,
  );

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <App invitationToken={invitationToken} />
    </BrowserRouter>
  </StrictMode>,
);
