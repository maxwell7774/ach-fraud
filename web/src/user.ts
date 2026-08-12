import { createSignal } from "solid-js";
import type { Me } from "./api";

// Shared reactive session state, set by the app gate after /api/auth/me.
export const [currentUser, setCurrentUser] = createSignal<Me | null>(null);
// authDisabled is true when Entra isn't configured (dev/e2e); the UI then
// shows everything with no role restrictions.
export const [authDisabled, setAuthDisabled] = createSignal(false);

// isAdmin is true when the user may see the admin surface (files, entries,
// headers, artifacts): admins and super admins.
export const isAdmin = () =>
  authDisabled() ||
  currentUser()?.role === "admin" ||
  currentUser()?.role === "super_admin";

// isSuperAdmin is true only for super admins, who additionally see events and
// manage email alert recipients.
export const isSuperAdmin = () => authDisabled() || currentUser()?.role === "super_admin";

export const canReview = () =>
  authDisabled() ||
  currentUser()?.role === "admin" ||
  currentUser()?.role === "super_admin" ||
  currentUser()?.role === "processor";
