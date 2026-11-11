import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { App } from "./App";
import "./styles.css";

import { captureLink } from "./auth/capability";
const link = captureLink(window.location, window.history);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <App {...link} />
    </BrowserRouter>
  </StrictMode>,
);
