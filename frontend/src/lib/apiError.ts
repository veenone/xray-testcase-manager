import { errMsg } from "../api";

// ApiError is the single error type the data layer throws, so callers can
// pattern-match one shape instead of the raw Wails error grab-bag.
export class ApiError extends Error {
  constructor(
    message: string,
    public readonly cause?: unknown,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// normalizeError turns any thrown value into an ApiError, reusing the existing
// errMsg extractor so Wails error shapes still produce a readable message.
export function normalizeError(e: unknown): ApiError {
  if (e instanceof ApiError) return e;
  const message = errMsg(e) || "Unknown error";
  return new ApiError(message, e);
}
