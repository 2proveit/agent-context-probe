import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import Index from "./routes/_index";
import "./tailwind.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <Index />
  </StrictMode>
);
