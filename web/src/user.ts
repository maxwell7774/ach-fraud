import { createSignal } from "solid-js";
import type { Me } from "./api";

// Shared reactive session state, set by the app gate after /api/auth/me.
export const [currentUser, setCurrentUser] = createSignal<Me | null>(null);
// authDisabled is true when Entra isn't configured (dev/e2e); the UI then
// shows everything with no role restrictions.
export const [authDisabled, setAuthDisabled] = createSignal(false);

// Everyone who is signed in may view the dashboard, holds, files, and
// entries; capabilities differ only by action (see canReview, canFlagEntry,
// canViewRaw, isSuperAdmin). In dev mode with auth disabled everything is
// visible.

// isSuperAdmin is true only for super admins, who additionally see events,
// manage email alert recipients, requeue jobs, and view raw ACH bytes.
export const isSuperAdmin = () => authDisabled() || currentUser()?.role === "super_admin";

export const canReview = () =>
  authDisabled() ||
  currentUser()?.role === "admin" ||
  currentUser()?.role === "super_admin" ||
  currentUser()?.role === "processor";

// canFlagEntry is true when the user may manually flag an entry for review:
// admins and super admins. Processors cannot.
export const canFlagEntry = () =>
  authDisabled() ||
  currentUser()?.role === "admin" ||
  currentUser()?.role === "super_admin";

// canViewRaw is true only for super admins, who may view raw ACH file bytes.
export const canViewRaw = () => authDisabled() || currentUser()?.role === "super_admin";
