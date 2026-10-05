import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./index.css";

const container = document.getElementById("root");

if (container === null) {
  throw new Error("Could not find the #root element to mount Stock Lens.");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
