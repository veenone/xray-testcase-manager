import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import "./style.css";
import App from "./App";
import { queryClient } from "./lib/queryClient";
import { DialogProvider } from "./contexts/DialogContext";
import { ProfileProvider } from "./contexts/ProfileContext";
import { SyncProvider } from "./contexts/SyncContext";
import { SelectionProvider } from "./contexts/SelectionContext";
import { NavProvider } from "./contexts/NavContext";
import { ModalProvider } from "./contexts/ModalContext";

const container = document.getElementById("root");

const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <DialogProvider>
        <ProfileProvider>
          <SyncProvider>
            <NavProvider>
              <SelectionProvider>
                <ModalProvider>
                  <App />
                </ModalProvider>
              </SelectionProvider>
            </NavProvider>
          </SyncProvider>
        </ProfileProvider>
      </DialogProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
