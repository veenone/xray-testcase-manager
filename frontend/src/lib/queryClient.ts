import { QueryClient } from "@tanstack/react-query";

// Tuned for local IPC, not network: the backend is a fast local Go process and
// Jira is synced on demand, so we do not retry or refetch in the background —
// freshness comes from explicit invalidation after mutations.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 0,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
});
