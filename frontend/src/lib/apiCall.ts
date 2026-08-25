import { normalizeError } from "./apiError";

// call wraps a binding invocation so every failure surfaces as an ApiError.
// Query/mutation functions use this instead of calling bindings directly.
export async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (e) {
    throw normalizeError(e);
  }
}
