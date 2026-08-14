import { query, action } from "@solidjs/router";
import { api, type ListParams, type RecipientInput } from "./api";

// Reads. Queries are keyed by name + arguments, so sharing (e.g. the dashboard
// on Dashboard and Holds) is deduplicated and actions revalidate them.
export const dashboardQuery = query(() => api.dashboard(), "dashboard");
export const holdsQuery = query((p: ListParams) => api.holds(p), "holds");
export const holdQuery = query((id: string) => api.hold(id), "hold");
export const submissionsQuery = query((p: ListParams) => api.submissions(p), "submissions");
export const submissionQuery = query(
  (p: { id: string } & ListParams) => api.submission(p.id, p),
  "submission"
);
export const verifyQuery = query(
  (p: { id: string } & ListParams) => api.verifySubmission(p.id, p),
  "verify"
);
export const entriesQuery = query((p: ListParams) => api.entries(p), "entries");
export const headersQuery = query((p: ListParams) => api.headers(p), "headers");
export const eventsQuery = query((p: ListParams) => api.events(p), "events");
export const jobsQuery = query((p: ListParams) => api.jobs(p), "jobs");
export const recipientsQuery = query(() => api.recipients(), "recipients");

// Mutations. After a successful action, Solid Router revalidates the queries
// used on the page, so approve/decline refresh the holds list and dashboard
// counts automatically.
export const approveHoldAction = action(async (args: { id: string; note: string }) => {
  try {
    await api.approve(args.id, { note: args.note, actor: "" });
    return { ok: true as const };
  } catch (e) {
    return { ok: false as const, error: String(e) };
  }
}, "approveHold");

export const declineHoldAction = action(async (args: { id: string; note: string }) => {
  try {
    await api.decline(args.id, { note: args.note, actor: "" });
    return { ok: true as const };
  } catch (e) {
    return { ok: false as const, error: String(e) };
  }
}, "declineHold");

export const bulkHoldAction = action(
  async (args: { action: "approve" | "decline"; ids: string[] }) => {
    try {
      const res = await api.bulk(args.action, args.ids, { note: "", actor: "" });
      return { ok: true as const, count: res.count };
    } catch (e) {
      return { ok: false as const, error: String(e) };
    }
  },
  "bulkHold"
);

export const logoutAction = action(async () => {
  await api.logout();
  return { ok: true };
}, "logout");

export const createRecipientAction = action(async (args: RecipientInput) => {
  try {
    await api.createRecipient(args);
    return { ok: true as const };
  } catch (e) {
    return { ok: false as const, error: String(e) };
  }
}, "createRecipient");

export const updateRecipientAction = action(
  async (args: RecipientInput & { id: string }) => {
    try {
      await api.updateRecipient(args.id, args);
      return { ok: true as const };
    } catch (e) {
      return { ok: false as const, error: String(e) };
    }
  },
  "updateRecipient"
);

export const deleteRecipientAction = action(async (args: { id: string }) => {
  try {
    await api.deleteRecipient(args.id);
    return { ok: true as const };
  } catch (e) {
    return { ok: false as const, error: String(e) };
  }
}, "deleteRecipient");

export const requeueJobAction = action(async (args: { id: string }) => {
  try {
    await api.requeueJob(args.id);
    return { ok: true as const };
  } catch (e) {
    return { ok: false as const, error: String(e) };
  }
}, "requeueJob");
