import { createSignal } from "solid-js";
import type { Me } from "./api";

// Shared reactive session state, set by the app gate after /api/auth/me.
export const [currentUser, setCurrentUser] = createSignal<Me | null>(null);
// authDisabled is true when Entra isn't configured (dev/e2e); the UI then
// shows everything with no role restrictions.
export const [authDisabled, setAuthDisabled] = createSignal(false);

export const isAdmin = () => authDisabled() || currentUser()?.role === "admin";

export const canReview = () =>
  authDisabled() || currentUser()?.role === "admin" || currentUser()?.role === "processor";
