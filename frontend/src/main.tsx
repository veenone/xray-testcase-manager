import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import "./style.css";
import App from "./App";
import { queryClient } from "./lib/queryClient";
import { DialogProvider } from "./contexts/DialogContext";

const container = document.getElementById("root");

const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <DialogProvider>
        <App />
      </DialogProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
